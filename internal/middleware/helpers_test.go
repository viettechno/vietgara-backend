package middleware_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const testSigningKey = "test-signing-key"

func newTestIssuer(t *testing.T) *auth.TokenIssuer {
	t.Helper()
	return auth.NewTokenIssuer([]byte(testSigningKey), 15*time.Minute, 24*time.Hour)
}

func issueToken(t *testing.T, issuer *auth.TokenIssuer, accountID string, garages []auth.GarageClaim) string {
	t.Helper()

	token, _, err := issuer.IssueAccessToken(accountID, garages)
	require.NoError(t, err)

	return token
}

// authedCtx returns the context produced by AuthInterceptor for the given
// issuer's token, mirroring how service entrypoints seed authenticated
// contexts from gRPC metadata.
func authedCtx(t *testing.T, issuer *auth.TokenIssuer, token string) context.Context {
	t.Helper()
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("authorization", token))

	gotCtx := make(chan context.Context, 1)
	interceptor := middleware.AuthInterceptor(issuer)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, req any) (any, error) {
		gotCtx <- ctx
		return nil, nil
	})
	require.NoError(t, err)

	return <-gotCtx
}

// testHandler returns a handler that delivers the context it receives to
// captured, so tests can inspect the context built by the interceptor.
func testHandler(captured chan<- context.Context) grpc.UnaryHandler {
	return func(ctx context.Context, req any) (any, error) {
		captured <- ctx
		return "handled", nil
	}
}
