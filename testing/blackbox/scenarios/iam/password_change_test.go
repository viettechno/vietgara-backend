package iam_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-IAM-036, Release 10.4, a wrong current password changes
// nothing, a good change revokes the other sessions and answers a new one, and
// a user with no password has nothing to change (FR-IAM-13).
func TestChangePassword(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "iam036")
	newPassword := owner.Password + "-new"

	otherSession, err := fixture.Anonymous(t).Login(ctx, owner.Email, owner.Password)
	require.NoError(t, err)

	_, err = owner.API.ChangePassword(ctx, "wrong-password", newPassword)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PASSWORD_INCORRECT")

	_, err = owner.API.ChangePassword(ctx, owner.Password, "short")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	fresh, err := owner.API.ChangePassword(ctx, owner.Password, newPassword)
	require.NoError(t, err)
	assert.NotEmpty(t, fresh.GetAccessToken())

	for name, refreshToken := range map[string]string{
		"the session that changed it": owner.Session.GetRefreshToken(),
		"another session":             otherSession.GetRefreshToken(),
	} {
		_, err = fixture.Anonymous(t).Refresh(ctx, refreshToken)
		fixture.RequireAPIError(t, err, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID")
		t.Logf("%s: revoked", name)
	}

	_, err = fixture.Anonymous(t).Refresh(ctx, fresh.GetRefreshToken())
	require.NoError(t, err, "the caller keeps a session")

	_, err = fixture.Anonymous(t).Login(ctx, owner.Email, owner.Password)
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "INVALID_CREDENTIALS")

	_, err = fixture.Anonymous(t).Login(ctx, owner.Email, newPassword)
	require.NoError(t, err, "the new password signs in")

	googleEmail := fixture.UniqueEmail(t, "iam036-google")

	googleOnly, err := fixture.Anonymous(t).SignInWithGoogleIDToken(ctx, googleToken(t, "google-iam036-"+googleEmail, googleEmail))
	require.NoError(t, err)

	_, err = fixture.Anonymous(t).WithToken(googleOnly.GetAccessToken()).ChangePassword(ctx, "anything", newPassword)
	fixture.RequireAPIError(t, err, http.StatusConflict, "NO_PASSWORD")
}
