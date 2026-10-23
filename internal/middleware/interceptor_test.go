package middleware_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

func TestChainUnaryInterceptors(t *testing.T) {
	t.Parallel()

	var order []string

	mark := func(name string) grpc.UnaryServerInterceptor {
		return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
			order = append(order, name)

			return next(ctx, req)
		}
	}

	resp, err := middleware.ChainUnaryInterceptors(mark("a"), mark("b"))(context.Background(), nil, info(methodPublic),
		func(context.Context, any) (any, error) {
			order = append(order, "handler")

			return "done", nil
		})
	require.NoError(t, err)
	assert.Equal(t, "done", resp)
	assert.Equal(t, []string{"a", "b", "handler"}, order)
}

func TestPolicyBuilders(t *testing.T) {
	t.Parallel()

	assert.True(t, middleware.PublicMethod().Public)
	assert.True(t, middleware.UnverifiedMethod().AllowUnverified)
	assert.Equal(t, access.PlatformPlanWrite, middleware.PlatformMethod(access.PlatformPlanWrite).Platform)
	assert.Equal(t, middleware.Policy{}, middleware.UserMethod())

	p := middleware.GarageMethod(access.Requires(access.CustomerRead))
	require.NotNil(t, p.Garage)
	assert.Equal(t, access.CustomerRead, p.Garage.Permission)

	merged := middleware.Merge(middleware.Policies{"a": middleware.PublicMethod()}, middleware.Policies{"b": middleware.PlatformMethod(access.PlatformPlanRead)})
	assert.Len(t, merged, 2)
}

func TestPlatformMethodNeedsAPermission(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() { PlatformMethod("") })
}
