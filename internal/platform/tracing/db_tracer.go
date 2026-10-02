package tracing

import (
	"context"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// QueryTracer is a pgx.QueryTracer that opens one span per query, so a
// slow or failing query is traceable within the request that caused it
// (NFR-OB-01). The SQL text is attached as an attribute -- safe, since it
// is always a literal with placeholders, never interpolated values --
// but query arguments are not, since they can carry customer PII.
type QueryTracer struct {
	tracer trace.Tracer
}

// NewQueryTracer returns a QueryTracer backed by provider (otel.GetTracerProvider()
// once Init has run, or a local provider in tests).
func NewQueryTracer(provider trace.TracerProvider) *QueryTracer {
	return &QueryTracer{tracer: provider.Tracer("vietgara/db")}
}

type queryTracerSpanKey struct{}

// TraceQueryStart implements pgx.QueryTracer.
func (t *QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := t.tracer.Start(ctx, "db.query", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("db.statement", data.SQL)))

	return context.WithValue(ctx, queryTracerSpanKey{}, span)
}

// TraceQueryEnd implements pgx.QueryTracer.
func (t *QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(queryTracerSpanKey{}).(trace.Span)
	if !ok {
		return
	}

	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}

	span.End()
}
