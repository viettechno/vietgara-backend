package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// Consumer defaults (NFR-AV-05: bounded retries with backoff).
const (
	DefaultMaxAttempts = 5
	DefaultBackoff     = 200 * time.Millisecond
	maxBackoff         = 10 * time.Second
)

var (
	errMalformedEvent = errors.New("malformed event")
	errNotRunning     = errors.New("kafka: consumer is not running")
)

// Consumer reads the topics of the subscribed event types in one consumer
// group and runs the handlers of each record. Offsets are committed only
// after a record was handled (or parked on its DLQ topic), so delivery is
// at-least-once; wrap handlers with outbox.Processor for exactly-once
// effects.
type Consumer struct {
	brokers     []string
	group       string
	prefix      string
	opts        []kgo.Opt
	maxAttempts int
	backoff     time.Duration

	mu       sync.RWMutex
	handlers map[string][]outbox.Handler
	admin    *kadm.Client // set while Run is consuming; reads the group's lag
}

// ConsumerOption customizes a Consumer.
type ConsumerOption func(*Consumer)

// WithRetry sets how often a failing handler is tried and the first
// backoff (doubled after each failure).
func WithRetry(attempts int, backoff time.Duration) ConsumerOption {
	return func(c *Consumer) { c.maxAttempts, c.backoff = attempts, backoff }
}

// WithClientOpts adds franz-go client options.
func WithClientOpts(opts ...kgo.Opt) ConsumerOption {
	return func(c *Consumer) { c.opts = append(c.opts, opts...) }
}

// NewConsumer returns a Consumer of group; call Subscribe, then Run.
func NewConsumer(brokers []string, group, prefix string, opts ...ConsumerOption) *Consumer {
	c := &Consumer{
		brokers: brokers, group: group, prefix: prefix,
		maxAttempts: DefaultMaxAttempts, backoff: DefaultBackoff,
		handlers: map[string][]outbox.Handler{},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Subscribe registers h for eventType (outbox.Subscriber).
func (c *Consumer) Subscribe(eventType string, h outbox.Handler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.handlers[eventType] = append(c.handlers[eventType], h)
}

// Topics lists the topics of the subscribed event types.
func (c *Consumer) Topics() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var topics []string

	for eventType := range c.handlers {
		if topic := TopicFor(c.prefix, eventType); !slices.Contains(topics, topic) {
			topics = append(topics, topic)
		}
	}

	slices.Sort(topics)

	return topics
}

// Run consumes until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	topics := c.Topics()
	if len(topics) == 0 {
		return nil
	}

	client, err := kgo.NewClient(append([]kgo.Opt{
		kgo.SeedBrokers(c.brokers...),
		kgo.ConsumerGroup(c.group),
		kgo.ConsumeTopics(topics...),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.AllowAutoTopicCreation(),
	}, c.opts...)...)
	if err != nil {
		return fmt.Errorf("kafka: consumer: %w", err)
	}
	defer client.Close()

	c.setAdmin(kadm.NewClient(client))
	defer c.setAdmin(nil)

	for {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil
		}

		fetches.EachError(func(topic string, partition int32, err error) {
			slog.Warn("kafka: fetch failed", "topic", topic, "partition", partition, "error", err)
		})

		fetches.EachRecord(func(record *kgo.Record) {
			c.handleRecord(ctx, client, record)
		})

		if err := client.CommitUncommittedOffsets(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("kafka: committing offsets failed", "group", c.group, "error", err)
		}
	}
}

func (c *Consumer) setAdmin(admin *kadm.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.admin = admin
}

// Lag returns, per subscribed topic, how many records the group has not
// committed yet (the consumer lag of NFR-OB-02's alerting). It fails while
// the consumer is not running.
func (c *Consumer) Lag(ctx context.Context) (map[string]int64, error) {
	c.mu.RLock()
	admin := c.admin
	c.mu.RUnlock()

	if admin == nil {
		return nil, errNotRunning
	}

	lags, err := admin.Lag(ctx, c.group)
	if err != nil {
		return nil, fmt.Errorf("kafka: group lag: %w", err)
	}

	group := lags[c.group]
	if err := group.Error(); err != nil {
		return nil, fmt.Errorf("kafka: group lag: %w", err)
	}

	byTopic := make(map[string]int64, len(group.Lag))
	for topic, total := range group.Lag.TotalByTopic() {
		byTopic[topic] = total.Lag
	}

	return byTopic, nil
}

// handleRecord runs every handler of the record's event type, retrying
// each with backoff; a handler that keeps failing parks the record on the
// DLQ topic so the partition is not blocked.
func (c *Consumer) handleRecord(ctx context.Context, client *kgo.Client, record *kgo.Record) {
	var event outbox.Event
	if err := json.Unmarshal(record.Value, &event); err != nil || event.ID == "" {
		c.park(ctx, client, record, errors.Join(errMalformedEvent, err))

		return
	}

	c.mu.RLock()
	handlers := append([]outbox.Handler(nil), c.handlers[event.Type]...)
	c.mu.RUnlock()

	for _, h := range handlers {
		if err := c.retry(ctx, h, event); err != nil {
			if ctx.Err() != nil {
				return
			}

			c.park(ctx, client, record, err)

			return
		}
	}
}

func (c *Consumer) retry(ctx context.Context, h outbox.Handler, event outbox.Event) error {
	backoff := c.backoff

	var err error

	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if err = h(ctx, event); err == nil {
			return nil
		}

		slog.Warn("kafka: handler failed", "event_id", event.ID, "event_type", event.Type, "attempt", attempt, "error", err)

		if attempt == c.maxAttempts {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}

	return err
}

func (c *Consumer) park(ctx context.Context, client *kgo.Client, record *kgo.Record, cause error) {
	dlq := &kgo.Record{
		Topic: DLQTopic(record.Topic),
		Key:   record.Key,
		Value: record.Value,
		Headers: append(slices.Clone(record.Headers),
			kgo.RecordHeader{Key: "error", Value: []byte(cause.Error())},
			kgo.RecordHeader{Key: "consumer_group", Value: []byte(c.group)},
		),
	}

	if err := client.ProduceSync(ctx, dlq).FirstErr(); err != nil {
		slog.Error("kafka: parking a record on the DLQ failed", "topic", dlq.Topic, "error", err)

		return
	}

	slog.Error("kafka: record parked on the DLQ", "topic", dlq.Topic, "error", cause)
}
