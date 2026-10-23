package middleware_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

var policies = middleware.Policies{
	methodPublic:         middleware.PublicMethod(),
	methodUser:           middleware.UserMethod(),
	"/test.v1.Svc/GetMe": middleware.UnverifiedMethod(),
}

// chain runs Auth and EmailVerified.
func chain() grpc.UnaryServerInterceptor {
	return middleware.ChainUnaryInterceptors(middleware.Auth(issuer, policies), middleware.EmailVerified())
}

func TestAuthPipeline(t *testing.T) {
	t.Parallel()

	verified := token(t, auth.Identity{UserID: "user-1", EmailVerified: true})
	unverified := token(t, auth.Identity{UserID: "user-2"})

	tests := []struct {
		name       string
		method     string
		ctx        context.Context
		wantCode   codes.Code
		wantReason string
		wantID     string
	}{
		{name: "public without token", method: methodPublic, ctx: context.Background()},
		{name: "unknown method fails closed", method: "/test.v1.Svc/Nope", ctx: context.Background(), wantCode: codes.PermissionDenied, wantReason: middleware.ReasonNoPolicy},
		{name: "no metadata", method: methodUser, ctx: context.Background(), wantCode: codes.Unauthenticated, wantReason: "UNAUTHENTICATED"},
		{name: "bad token", method: methodUser, ctx: withToken(context.Background(), "garbage"), wantCode: codes.Unauthenticated, wantReason: "UNAUTHENTICATED"},
		{name: "verified user", method: methodUser, ctx: withToken(context.Background(), verified), wantID: "user-1"},
		{name: "unverified blocked", method: methodUser, ctx: withToken(context.Background(), unverified), wantCode: codes.PermissionDenied, wantReason: middleware.ReasonEmailNotVerified},
		{name: "unverified allowed", method: "/test.v1.Svc/GetMe", ctx: withToken(context.Background(), unverified), wantID: "user-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotID string

			_, err := chain()(tt.ctx, nil, info(tt.method), func(ctx context.Context, _ any) (any, error) {
				gotID = middleware.UserID(ctx)

				return nil, nil
			})

			if tt.wantCode != codes.OK {
				requireCode(t, err, tt.wantCode, tt.wantReason)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, gotID)
		})
	}
}

func TestInterceptorsWithoutAuth(t *testing.T) {
	t.Parallel()

	// Without Auth in front there is no policy: nothing is gated.
	ran := false
	_, err := middleware.EmailVerified()(context.Background(), nil, info(methodUser), okHandler(&ran))
	require.Error(t, err, "no policy means a signed-in user is required")
}

func TestClaimsHelpers(t *testing.T) {
	t.Parallel()

	assert.Empty(t, middleware.UserID(context.Background()))

	_, ok := middleware.ClaimsFromContext(context.Background())
	assert.False(t, ok)

	claims := &auth.Claims{}
	claims.Subject = "user-9"
	ctx := middleware.WithClaims(context.Background(), claims)
	assert.Equal(t, "user-9", middleware.UserID(ctx))
}
