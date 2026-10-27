package observability

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

const (
	// sampleTTL bounds how often a scrape reaches the database or the broker:
	// several Prometheus replicas, or a manual curl, must not multiply it.
	sampleTTL     = 5 * time.Second
	sampleTimeout = 3 * time.Second
)

// sampled is a collector for values that cost a query to read. The result is
// reused for sampleTTL; a failed sample exports nothing and is counted, so a
// dead database shows as missing series plus the error counter, never as a
// stale healthy number.
type sampled struct {
	name   string
	descs  []*prometheus.Desc
	sample func(ctx context.Context) ([]prometheus.Metric, error)
	errs   prometheus.Counter

	mu     sync.Mutex
	at     time.Time
	cached []prometheus.Metric
}

func newSampled(name string, descs []*prometheus.Desc, sample func(context.Context) ([]prometheus.Metric, error)) *sampled {
	return &sampled{
		name: name, descs: descs, sample: sample,
		errs: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "vietgara_collector_errors_total", Help: "Failed samples of a metrics collector.",
			ConstLabels: prometheus.Labels{"collector": name},
		}),
	}
}

func (s *sampled) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range s.descs {
		ch <- d
	}

	s.errs.Describe(ch)
}

func (s *sampled) Collect(ch chan<- prometheus.Metric) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if time.Since(s.at) >= sampleTTL {
		ctx, cancel := context.WithTimeout(context.Background(), sampleTimeout)
		metrics, err := s.sample(ctx)

		cancel()

		s.at = time.Now()

		if err != nil {
			slog.Warn("metrics: sampling failed", "collector", s.name, "error", err)
			s.errs.Inc()

			metrics = nil
		}

		s.cached = metrics
	}

	for _, m := range s.cached {
		ch <- m
	}

	s.errs.Collect(ch)
}

// NewPoolCollector exports the database connection pool, read at scrape time.
func NewPoolCollector(stat func() *pgxpool.Stat) prometheus.Collector {
	conns := prometheus.NewDesc("vietgara_db_pool_connections", "Open pool connections by state (acquired, idle).", []string{"state"}, nil)
	maxConns := prometheus.NewDesc("vietgara_db_pool_max_connections", "Largest number of connections the pool opens.", nil, nil)
	acquires := prometheus.NewDesc("vietgara_db_pool_acquires_total", "Connection acquisitions.", nil, nil)
	waits := prometheus.NewDesc("vietgara_db_pool_empty_acquires_total", "Acquisitions that had to wait for a free connection.", nil, nil)
	waited := prometheus.NewDesc("vietgara_db_pool_acquire_wait_seconds_total", "Time spent waiting for a free connection.", nil, nil)

	return poolCollector{stat: stat, descs: []*prometheus.Desc{conns, acquires, waits, waited, maxConns}}
}

type poolCollector struct {
	stat  func() *pgxpool.Stat
	descs []*prometheus.Desc
}

func (c poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range c.descs {
		ch <- d
	}
}

func (c poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.stat()
	if s == nil {
		return
	}

	for state, n := range map[string]int32{"acquired": s.AcquiredConns(), "idle": s.IdleConns()} {
		ch <- prometheus.MustNewConstMetric(c.descs[0], prometheus.GaugeValue, float64(n), state)
	}

	ch <- prometheus.MustNewConstMetric(c.descs[4], prometheus.GaugeValue, float64(s.MaxConns()))

	ch <- prometheus.MustNewConstMetric(c.descs[1], prometheus.CounterValue, float64(s.AcquireCount()))

	ch <- prometheus.MustNewConstMetric(c.descs[2], prometheus.CounterValue, float64(s.EmptyAcquireCount()))

	ch <- prometheus.MustNewConstMetric(c.descs[3], prometheus.CounterValue, s.AcquireDuration().Seconds())
}

// NewOutboxCollector exports the outbox backlog: pending events, the age of
// the oldest and the events parked as failed.
func NewOutboxCollector(read func(context.Context) (outbox.Backlog, error)) prometheus.Collector {
	pending := prometheus.NewDesc("vietgara_outbox_pending_events", "Outbox events waiting for delivery.", nil, nil)
	oldest := prometheus.NewDesc("vietgara_outbox_oldest_pending_age_seconds", "Age of the oldest pending outbox event, 0 when none waits.", nil, nil)
	failed := prometheus.NewDesc("vietgara_outbox_failed_events", "Outbox events parked as failed.", nil, nil)

	return newSampled("outbox", []*prometheus.Desc{pending, oldest, failed}, func(ctx context.Context) ([]prometheus.Metric, error) {
		b, err := read(ctx)
		if err != nil {
			return nil, err
		}

		return []prometheus.Metric{
			prometheus.MustNewConstMetric(pending, prometheus.GaugeValue, float64(b.Pending)),
			prometheus.MustNewConstMetric(oldest, prometheus.GaugeValue, b.OldestPending.Seconds()),
			prometheus.MustNewConstMetric(failed, prometheus.GaugeValue, float64(b.Failed)),
		}, nil
	})
}

// NewConsumerLagCollector exports the event consumer's lag per topic.
func NewConsumerLagCollector(read func(context.Context) (map[string]int64, error)) prometheus.Collector {
	lag := prometheus.NewDesc("vietgara_kafka_consumer_lag", "Records the consumer group has not committed, by topic. The value is group-wide: use max by (topic) across replicas.", []string{"topic"}, nil)

	return newSampled("kafka_lag", []*prometheus.Desc{lag}, func(ctx context.Context) ([]prometheus.Metric, error) {
		byTopic, err := read(ctx)
		if err != nil {
			return nil, err
		}

		out := make([]prometheus.Metric, 0, len(byTopic))
		for topic, n := range byTopic {
			out = append(out, prometheus.MustNewConstMetric(lag, prometheus.GaugeValue, float64(n), topic))
		}

		return out, nil
	})
}
