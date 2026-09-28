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
	methodAccount:        middleware.AccountMethod(),
	"/test.v1.Svc/GetMe": middleware.UnverifiedMethod(),
	"/test.v1.Svc/Admin": middleware.AdminMethod(),
}

// chain runs Auth, EmailVerified and PlatformAdmin.
func chain() grpc.UnaryServerInterceptor {
	return middleware.ChainUnaryInterceptors(middleware.Auth(issuer, policies), middleware.EmailVerified(), middleware.PlatformAdmin())
}

func TestAuthPipeline(t *testing.T) {
	t.Parallel()

	verified := token(t, auth.Identity{AccountID: "acc-1", EmailVerified: true})
	unverified := token(t, auth.Identity{AccountID: "acc-2"})
	admin := token(t, auth.Identity{AccountID: "acc-3", EmailVerified: true, PlatformAdmin: true})

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
		{name: "no metadata", method: methodAccount, ctx: context.Background(), wantCode: codes.Unauthenticated, wantReason: "UNAUTHENTICATED"},
		{name: "bad token", method: methodAccount, ctx: withToken(context.Background(), "garbage"), wantCode: codes.Unauthenticated, wantReason: "UNAUTHENTICATED"},
		{name: "verified account", method: methodAccount, ctx: withToken(context.Background(), verified), wantID: "acc-1"},
		{name: "unverified blocked", method: methodAccount, ctx: withToken(context.Background(), unverified), wantCode: codes.PermissionDenied, wantReason: middleware.ReasonEmailNotVerified},
		{name: "unverified allowed", method: "/test.v1.Svc/GetMe", ctx: withToken(context.Background(), unverified), wantID: "acc-2"},
		{name: "admin method for non-admin", method: "/test.v1.Svc/Admin", ctx: withToken(context.Background(), verified), wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED"},
		{name: "admin method for admin", method: "/test.v1.Svc/Admin", ctx: withToken(context.Background(), admin), wantID: "acc-3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotID string

			_, err := chain()(tt.ctx, nil, info(tt.method), func(ctx context.Context, _ any) (any, error) {
				gotID = middleware.AccountID(ctx)

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
	_, err := middleware.EmailVerified()(context.Background(), nil, info(methodAccount), okHandler(&ran))
	require.Error(t, err, "no policy means a signed-in account is required")

	ran = false
	_, err = middleware.PlatformAdmin()(context.Background(), nil, info(methodAccount), okHandler(&ran))
	require.NoError(t, err)
	assert.True(t, ran)
}

func TestClaimsHelpers(t *testing.T) {
	t.Parallel()

	assert.Empty(t, middleware.AccountID(context.Background()))

	_, ok := middleware.ClaimsFromContext(context.Background())
	assert.False(t, ok)

	claims := &auth.Claims{}
	claims.Subject = "acc-9"
	ctx := middleware.WithClaims(context.Background(), claims)
	assert.Equal(t, "acc-9", middleware.AccountID(ctx))
}
