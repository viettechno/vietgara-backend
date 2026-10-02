// Package tracing wires OpenTelemetry distributed tracing (NFR-OB-01):
// every request gets a trace id that is logged and, once an OTLP
// collector is configured, exported -- the modular monolith is still a
// single process, so this gives request-scoped correlation now and a
// path to real cross-service tracing once modules split into their own
// services.
package tracing

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Shutdown flushes and stops the tracer provider; call it on graceful
// shutdown so buffered spans are not lost.
type Shutdown func(ctx context.Context) error

// Init sets the global TracerProvider and propagator. Without endpoint,
// spans are still created (so every request still carries a trace id to
// log) but are not exported anywhere -- the same "work standalone, export
// once configured" shape as Kafka and Redis.
func Init(ctx context.Context, serviceName, endpoint string) (Shutdown, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, err
	}

	opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}

	if endpoint != "" {
		exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure())
		if err != nil {
			return nil, err
		}

		opts = append(opts, sdktrace.WithBatcher(exporter))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	return func(ctx context.Context) error {
		return errors.Join(tp.ForceFlush(ctx), tp.Shutdown(ctx))
	}, nil
}
