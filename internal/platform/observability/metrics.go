package observability

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	labelModule = "module"
	// maxCauseLen bounds the cause in a log line; a driver error can quote a statement.
	maxCauseLen = 500
)

// Metrics holds the registry and the request instruments. The REST API
// reaches the same gRPC handlers, so one interceptor measures both.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	faults   *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewMetrics registers the request instruments and the Go and process
// collectors.
func NewMetrics() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vietgara_requests_total", Help: "Requests by module and gRPC status code.",
		}, []string{labelModule, "code"}),
		faults: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "vietgara_request_errors_total", Help: "Requests that failed on the server side (HTTP 5xx), by module.",
		}, []string{labelModule}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "vietgara_request_duration_seconds", Help: "Request latency by module.",
			// NFR-PF-01/02 targets are 0.3 s and 0.8 s: keep buckets at both.
			Buckets: []float64{.005, .01, .025, .05, .1, .2, .3, .5, .8, 1.5, 3, 6},
		}, []string{labelModule}),
	}

	m.registry.MustRegister(m.requests, m.faults, m.duration,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return m
}

// Register adds a collector (pool, outbox, consumer lag) to the registry.
func (m *Metrics) Register(c prometheus.Collector) { m.registry.MustRegister(c) }

// Handler serves the metrics in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Observe is the outermost interceptor: it counts and times the request per
// module, and writes one log line with the trace id, the garage id, the
// outcome and, for a server-side fault, the cause that the client never sees.
func (m *Metrics) Observe() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = withRequest(ctx)
		start := time.Now()

		resp, err := handler(ctx, req)

		elapsed := time.Since(start)
		code := status.Code(err)
		module := moduleOf(info.FullMethod)

		m.requests.WithLabelValues(module, code.String()).Inc()
		m.duration.WithLabelValues(module).Observe(elapsed.Seconds())

		attrs := []any{"method", info.FullMethod, "module", module, "code", code.String(), "duration_ms", elapsed.Milliseconds()}

		switch {
		case isServerFault(code):
			m.faults.WithLabelValues(module).Inc()

			if cause := apperror.InternalCause(err); cause != nil {
				attrs = append(attrs, "error", truncate(cause.Error(), maxCauseLen))
			}

			slog.ErrorContext(ctx, "request failed", attrs...)
		case code == codes.OK:
			slog.InfoContext(ctx, "request", attrs...)
		default:
			slog.WarnContext(ctx, "request rejected", attrs...)
		}

		return resp, err
	}
}

// isServerFault reports the gRPC codes that grpc-gateway answers as HTTP 5xx.
func isServerFault(code codes.Code) bool {
	switch code { //nolint:exhaustive // every other code is a client-side outcome
	case codes.Unknown, codes.Internal, codes.DataLoss, codes.Unimplemented, codes.Unavailable, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

// moduleOf returns the module of "/vietgara.<module>.v1.<Service>/<Method>",
// the second element of the proto package.
func moduleOf(fullMethod string) string {
	pkg := strings.TrimPrefix(fullMethod, "/")
	if i := strings.IndexByte(pkg, '/'); i >= 0 {
		pkg = pkg[:i]
	}

	if parts := strings.Split(pkg, "."); len(parts) >= 3 && parts[0] == "vietgara" {
		return parts[1]
	}

	return "other"
}

// truncate cuts s to at most n bytes, on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}

	return s[:n] + "…"
}
