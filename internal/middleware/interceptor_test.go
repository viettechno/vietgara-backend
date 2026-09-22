package middleware_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"google.golang.org/grpc"
)

func TestChainUnaryInterceptors(t *testing.T) {
	tests := []struct {
		name      string
		failFirst bool
		wantOrder []string
		wantErr   bool
	}{
		{
			name:      "interceptors run in declared order",
			wantOrder: []string{"first-before", "second-before", "third-before", "handler", "third-after", "second-after", "first-after"},
		},
		{
			name:      "error short circuits inner interceptors",
			failFirst: true,
			wantOrder: []string{"first-before"},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var order []string

			recorder := func(name string, fail bool) grpc.UnaryServerInterceptor {
				return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
					order = append(order, name+"-before")

					if fail {
						return nil, errors.New("interceptor failed")
					}

					resp, err := handler(ctx, req)

					order = append(order, name+"-after")

					return resp, err
				}
			}

			chain := middleware.ChainUnaryInterceptors(
				recorder("first", tt.failFirst),
				recorder("second", false),
				recorder("third", false),
			)

			_, err := chain(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: testRoute}, func(ctx context.Context, req any) (any, error) {
				order = append(order, "handler")
				return "done", nil
			})

			assert.Equal(t, tt.wantOrder, order)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}
