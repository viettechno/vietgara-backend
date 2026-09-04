package middleware

import (
	"context"
	"slices"

	"google.golang.org/grpc"
)

// ChainUnaryInterceptors composes multiple interceptors into one, applied
// in the given order (first argument runs outermost).
func ChainUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		chain := handler

		for _, interceptor := range slices.Backward(interceptors) {
			next := chain

			chain = func(ctx context.Context, req any) (any, error) {
				return interceptor(ctx, req, info, next)
			}
		}

		return chain(ctx, req)
	}
}
