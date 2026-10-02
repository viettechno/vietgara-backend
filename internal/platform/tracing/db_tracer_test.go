package tracing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	otelcodes "go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/viettechno/vietgara-backend/internal/platform/tracing"
)

func TestQueryTracer(t *testing.T) {
	t.Parallel()

	t.Run("records a successful query as a span", func(t *testing.T) {
		t.Parallel()

		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		qt := tracing.NewQueryTracer(provider)

		ctx := qt.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
		qt.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 1")})

		spans := recorder.Ended()
		require.Len(t, spans, 1)
		require.Equal(t, "db.query", spans[0].Name())
		require.Equal(t, otelcodes.Unset, spans[0].Status().Code)
	})

	t.Run("records a failing query's error on the span", func(t *testing.T) {
		t.Parallel()

		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		qt := tracing.NewQueryTracer(provider)

		ctx := qt.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1/0"})
		qt.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("division by zero")})

		spans := recorder.Ended()
		require.Len(t, spans, 1)
		require.Equal(t, otelcodes.Error, spans[0].Status().Code)
	})

	t.Run("TraceQueryEnd is a no-op without a matching start", func(t *testing.T) {
		t.Parallel()

		qt := tracing.NewQueryTracer(sdktrace.NewTracerProvider())

		require.NotPanics(t, func() {
			qt.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})
		})
	})
}
