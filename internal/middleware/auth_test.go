package middleware_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthInterceptor(t *testing.T) {
	issuer := newTestIssuer(t)
	validToken := issueToken(t, issuer, "acc-1", []auth.GarageClaim{{GarageID: "garage-1", Role: "owner"}})

	tests := []struct {
		name     string
		ctx      context.Context
		wantCode codes.Code
	}{
		{name: "missing metadata", ctx: t.Context(), wantCode: codes.Unauthenticated},
		{name: "missing authorization header", ctx: metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-other", "v")), wantCode: codes.Unauthenticated},
		{name: "invalid token", ctx: metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", "not-a-jwt")), wantCode: codes.Unauthenticated},
		{name: "valid token", ctx: metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", validToken)), wantCode: codes.OK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			interceptor := middleware.AuthInterceptor(issuer)
			handlerCtx := make(chan context.Context, 1)

			got, err := interceptor(tt.ctx, nil, &grpc.UnaryServerInfo{FullMethod: testRoute}, testHandler(handlerCtx))

			if tt.wantCode != codes.OK {
				assert.Equal(t, tt.wantCode, status.Code(err))
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "handled", got)

			claims, ok := middleware.ClaimsFromContext(<-handlerCtx)
			require.True(t, ok)
			assert.Equal(t, "acc-1", claims.Subject)
			assert.Len(t, claims.Garages, 1)
			assert.Equal(t, "garage-1", claims.Garages[0].GarageID)
			assert.Equal(t, "owner", claims.Garages[0].Role)
		})
	}
}

func TestAuthInterceptor_WithSkipMethods(t *testing.T) {
	issuer := newTestIssuer(t)

	t.Run("skipped method passes without any token", func(t *testing.T) {
		t.Parallel()

		interceptor := middleware.AuthInterceptor(issuer,
			middleware.WithSkipMethods("/vietgara.identity.v1.AuthService/Register"),
		)

		got, err := interceptor(t.Context(), nil, &grpc.UnaryServerInfo{
			FullMethod: "/vietgara.identity.v1.AuthService/Register",
		}, testHandler(make(chan context.Context, 1)))
		require.NoError(t, err)
		assert.Equal(t, "handled", got)
	})

	t.Run("unlisted method still requires a token", func(t *testing.T) {
		t.Parallel()

		interceptor := middleware.AuthInterceptor(issuer,
			middleware.WithSkipMethods("/vietgara.identity.v1.AuthService/Register"),
		)
		handlerCtx := make(chan context.Context, 1)

		_, err := interceptor(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: testRoute}, testHandler(handlerCtx))
		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("multiple skip methods are all allowed", func(t *testing.T) {
		t.Parallel()

		interceptor := middleware.AuthInterceptor(issuer,
			middleware.WithSkipMethods(
				"/vietgara.identity.v1.AuthService/Register",
				"/vietgara.identity.v1.AuthService/Login",
			),
		)

		for _, method := range []string{
			"/vietgara.identity.v1.AuthService/Register",
			"/vietgara.identity.v1.AuthService/Login",
		} {
			got, err := interceptor(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: method}, testHandler(make(chan context.Context, 1)))
			require.NoError(t, err)
			assert.Equal(t, "handled", got)
		}
	})
}
