package middleware_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"

	"github.com/viettechno/vietgara-backend/internal/middleware"
)

// TestClientIP covers the request metadata forwarded by the REST gateway.
func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{name: "no metadata", ctx: context.Background()},
		{name: "no header", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("a", "b"))},
		{name: "single address", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-forwarded-for", " 10.0.0.1 ")), want: "10.0.0.1"},
		{name: "proxy chain", ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-forwarded-for", "1.2.3.4, 10.0.0.1")), want: "1.2.3.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, middleware.ClientIP(tt.ctx))
		})
	}
}
