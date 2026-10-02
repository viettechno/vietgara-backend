package tracing_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/viettechno/vietgara-backend/internal/platform/tracing"
)

func TestInit(t *testing.T) {
	t.Run("without an endpoint, spans are still created but not exported", func(t *testing.T) {
		shutdown, err := tracing.Init(t.Context(), "vietgara-backend-test", "")
		require.NoError(t, err)

		t.Cleanup(func() { require.NoError(t, shutdown(context.Background())) })

		_, span := otel.Tracer("test").Start(t.Context(), "a-span")
		require.True(t, span.SpanContext().IsValid(), "every request should still carry a usable trace id")
		span.End()
	})

	t.Run("with an endpoint, the OTLP exporter is wired without erroring", func(t *testing.T) {
		shutdown, err := tracing.Init(t.Context(), "vietgara-backend-test", "localhost:4317")
		require.NoError(t, err)

		t.Cleanup(func() { require.NoError(t, shutdown(context.Background())) })
	})
}
