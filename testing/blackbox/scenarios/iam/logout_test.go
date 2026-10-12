package iam_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-IAM-009 (partial): signing out revokes the refresh token,
// so the session cannot be refreshed afterwards; revocation is idempotent
// and per-token, not per-user. Browser history and cached pages are web-E2E.
func TestLogoutRevokesSession(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	api := fixture.Anonymous(t)

	owner := fixture.NewVerifiedOwner(t, "logout")

	require.NoError(t, api.Logout(ctx, owner.Session.GetRefreshToken()))

	_, err := api.Refresh(ctx, owner.Session.GetRefreshToken())
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID")

	// Revoking twice still succeeds.
	require.NoError(t, api.Logout(ctx, owner.Session.GetRefreshToken()))

	// The user itself is unaffected: signing in again works.
	renewed, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)

	_, err = api.Refresh(ctx, renewed.GetRefreshToken())
	require.NoError(t, err)
}
