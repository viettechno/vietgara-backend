package observability

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// logHandler adds the trace id and the garage id of the context to every
// record logged with a context, so the central log store (NFR-OB-03) finds
// all lines of one request or one garage.
type logHandler struct{ slog.Handler }

// NewLogger returns a JSON logger for w whose records carry trace_id,
// span_id and garage_id when the context has them.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(logHandler{slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})})
}

func (h logHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}

	if garageID := GarageID(ctx); garageID != "" {
		r.AddAttrs(slog.String("garage_id", garageID))
	}

	return h.Handler.Handle(ctx, r)
}

func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return logHandler{h.Handler.WithAttrs(attrs)}
}

func (h logHandler) WithGroup(name string) slog.Handler {
	return logHandler{h.Handler.WithGroup(name)}
}
