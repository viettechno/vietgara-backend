package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/platform/observability"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

var errSample = errors.New("sample failed")

// logTo routes the default logger to a buffer for the test and restores it.
func logTo(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer

	previous := slog.Default()

	slog.SetDefault(observability.NewLogger(&buf, slog.LevelInfo))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return &buf
}

func lastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.NotEmpty(t, lines)

	var record map[string]any
	require.NoError(t, json.Unmarshal(lines[len(lines)-1], &record))

	return record
}

func scrape(t *testing.T, m *observability.Metrics) string {
	t.Helper()

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", http.NoBody))

	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)

	return string(body)
}

func TestObserve(t *testing.T) {
	const method = "/vietgara.quote.v1.QuoteService/CreateQuote"

	traceID := trace.TraceID{1, 2, 3}
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: trace.SpanID{4}, TraceFlags: trace.FlagsSampled,
	}))
	info := &grpc.UnaryServerInfo{FullMethod: method}

	run := func(m *observability.Metrics, err error) {
		_, _ = m.Observe()(ctx, nil, info, func(ctx context.Context, _ any) (any, error) {
			observability.SetGarage(ctx, "garage-1")

			return nil, err
		})
	}

	t.Run("a successful request is counted and logged with trace and garage id", func(t *testing.T) {
		buf := logTo(t)
		m := observability.NewMetrics()

		run(m, nil)

		out := scrape(t, m)
		assert.Contains(t, out, `vietgara_requests_total{code="OK",module="quote"} 1`)
		assert.Contains(t, out, `vietgara_request_duration_seconds_count{module="quote"} 1`)
		assert.NotContains(t, out, "vietgara_request_errors_total{")

		line := lastLine(t, buf)
		assert.Equal(t, "request", line["msg"])
		assert.Equal(t, "INFO", line["level"])
		assert.Equal(t, traceID.String(), line["trace_id"])
		assert.Equal(t, "garage-1", line["garage_id"])
		assert.Equal(t, method, line["method"])
	})

	t.Run("a client error is a warning, not a fault", func(t *testing.T) {
		buf := logTo(t)
		m := observability.NewMetrics()

		run(m, status.Error(codes.PermissionDenied, "no"))

		out := scrape(t, m)
		assert.Contains(t, out, `vietgara_requests_total{code="PermissionDenied",module="quote"} 1`)
		assert.NotContains(t, out, "vietgara_request_errors_total{")
		assert.Equal(t, "WARN", lastLine(t, buf)["level"])
	})

	t.Run("a server fault counts as an error and logs the cause the client never sees", func(t *testing.T) {
		buf := logTo(t)
		m := observability.NewMetrics()

		run(m, apperror.ToGRPCError(apperror.Internal("load quote", errSample)))

		assert.Contains(t, scrape(t, m), `vietgara_request_errors_total{module="quote"} 1`)

		line := lastLine(t, buf)
		assert.Equal(t, "ERROR", line["level"])
		assert.Equal(t, "INTERNAL: load quote: sample failed", line["error"])
	})

	t.Run("a method outside the vietgara packages is the module other", func(t *testing.T) {
		m := observability.NewMetrics()

		_, _ = m.Observe()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/grpc.reflection.v1.ServerReflection/Info"},
			func(context.Context, any) (any, error) { return nil, nil })

		assert.Contains(t, scrape(t, m), `module="other"`)
	})
}

func TestCollectors(t *testing.T) {
	t.Parallel()

	t.Run("outbox backlog is exported and a failed sample is counted without series", func(t *testing.T) {
		t.Parallel()

		m := observability.NewMetrics()
		calls := 0

		m.Register(observability.NewOutboxCollector(func(context.Context) (outbox.Backlog, error) {
			calls++
			if calls > 1 {
				return outbox.Backlog{}, errSample
			}

			return outbox.Backlog{Pending: 7, OldestPending: 42 * time.Second, Failed: 2}, nil
		}))

		out := scrape(t, m)
		assert.Contains(t, out, "vietgara_outbox_pending_events 7")
		assert.Contains(t, out, "vietgara_outbox_oldest_pending_age_seconds 42")
		assert.Contains(t, out, "vietgara_outbox_failed_events 2")

		// Within the sample TTL the second scrape reuses the first result.
		assert.Contains(t, scrape(t, m), "vietgara_outbox_pending_events 7")
		assert.Equal(t, 1, calls)
	})

	t.Run("a sample that fails exports no series and raises the error counter", func(t *testing.T) {
		t.Parallel()

		m := observability.NewMetrics()
		m.Register(observability.NewConsumerLagCollector(func(context.Context) (map[string]int64, error) { return nil, errSample }))

		out := scrape(t, m)
		assert.NotContains(t, out, "vietgara_kafka_consumer_lag{")
		assert.Contains(t, out, `vietgara_collector_errors_total{collector="kafka_lag"} 1`)
	})

	t.Run("consumer lag is exported per topic", func(t *testing.T) {
		t.Parallel()

		m := observability.NewMetrics()
		m.Register(observability.NewConsumerLagCollector(func(context.Context) (map[string]int64, error) {
			return map[string]int64{"vietgara.tenant.events": 12}, nil
		}))

		assert.Contains(t, scrape(t, m), `vietgara_kafka_consumer_lag{topic="vietgara.tenant.events"} 12`)
	})

	t.Run("a pool without statistics exports nothing", func(t *testing.T) {
		t.Parallel()

		m := observability.NewMetrics()
		m.Register(observability.NewPoolCollector(func() *pgxpool.Stat { return nil }))

		assert.NotContains(t, scrape(t, m), "vietgara_db_pool_connections")
	})
}
