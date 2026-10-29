package iam_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/stubs/google/idtoken"
)

// googleToken mints a Google ID token for subject, with a verified e-mail.
func googleToken(t *testing.T, subject, email string) string {
	t.Helper()

	token, err := fixture.GoogleStub(t).IDToken(t.Context(), idtoken.Claims{
		Subject: subject, Email: email, EmailVerified: true, Name: "Blackbox Google", Audience: env.Get(t).GoogleClientID,
	})
	require.NoError(t, err)

	return token
}

// Scenario: E2E-IAM-031, Release 10.3, a user with a password links a Google
// account and signs in with both; the user then shows both ways (FR-IAM-06).
func TestLinkGoogleAccount(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "iam031")
	subject := "google-iam031-" + owner.Email

	me, err := owner.API.Me(ctx)
	require.NoError(t, err)
	assert.False(t, me.GetGoogleLinked())
	assert.True(t, me.GetHasPassword())

	linked, err := owner.API.LinkGoogle(ctx, googleToken(t, subject, "someone."+owner.Email), owner.Password)
	require.NoError(t, err)
	assert.True(t, linked.GetGoogleLinked())
	assert.True(t, linked.GetHasPassword())
	assert.Equal(t, owner.Email, linked.GetEmail(), "the user keeps its own e-mail")

	viaGoogle, err := fixture.Anonymous(t).SignInWithGoogleIDToken(ctx, googleToken(t, subject, "someone."+owner.Email))
	require.NoError(t, err)
	assert.Equal(t, owner.Session.GetUser().GetId(), viaGoogle.GetUser().GetId(), "Google signs in to the linked user, whatever its e-mail")

	_, err = fixture.Anonymous(t).Login(ctx, owner.Email, owner.Password)
	require.NoError(t, err, "the password still works")

	again, err := owner.API.LinkGoogle(ctx, googleToken(t, subject, "someone."+owner.Email), owner.Password)
	require.NoError(t, err, "linking the same account again changes nothing")
	assert.True(t, again.GetGoogleLinked())
}

// Scenario: E2E-IAM-032, Release 10.3, a Google account belongs to one user,
// and a user has one Google account (FR-IAM-06).
func TestLinkGoogleConflicts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "iam032")
	other := fixture.NewVerifiedOwner(t, "iam032-other")

	_, err := other.API.LinkGoogle(ctx, googleToken(t, "google-iam032-taken-"+other.Email, other.Email), other.Password)
	require.NoError(t, err)

	_, err = owner.API.LinkGoogle(ctx, googleToken(t, "google-iam032-taken-"+other.Email, other.Email), owner.Password)
	fixture.RequireAPIError(t, err, http.StatusConflict, "GOOGLE_ALREADY_LINKED")

	_, err = owner.API.LinkGoogle(ctx, googleToken(t, "google-iam032-mine-"+owner.Email, owner.Email), owner.Password)
	require.NoError(t, err)

	_, err = owner.API.LinkGoogle(ctx, googleToken(t, "google-iam032-second-"+owner.Email, owner.Email), owner.Password)
	fixture.RequireAPIError(t, err, http.StatusConflict, "GOOGLE_LINK_EXISTS")
}

// Scenario: E2E-IAM-033, Release 10.3, the password confirms both; a user
// created by Google, who has none, cannot link or unlink, so the last way to
// sign in is kept (FR-IAM-06).
func TestGoogleLinkNeedsThePassword(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "iam033")

	_, err := owner.API.LinkGoogle(ctx, googleToken(t, "google-iam033-"+owner.Email, owner.Email), "wrong-password")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PASSWORD_INCORRECT")

	_, err = owner.API.UnlinkGoogle(ctx, owner.Password)
	fixture.RequireAPIError(t, err, http.StatusConflict, "GOOGLE_NOT_LINKED")

	_, err = owner.API.LinkGoogle(ctx, googleToken(t, "google-iam033-"+owner.Email, owner.Email), owner.Password)
	require.NoError(t, err)

	_, err = owner.API.UnlinkGoogle(ctx, "wrong-password")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PASSWORD_INCORRECT")

	me, err := owner.API.Me(ctx)
	require.NoError(t, err)
	assert.True(t, me.GetGoogleLinked(), "a refused unlink removes nothing")

	googleEmail := fixture.UniqueEmail(t, "iam033-google")

	googleOnly, err := fixture.Anonymous(t).SignInWithGoogleIDToken(ctx, googleToken(t, "google-iam033-only-"+googleEmail, googleEmail))
	require.NoError(t, err)
	assert.False(t, googleOnly.GetUser().GetHasPassword())

	api := fixture.Anonymous(t).WithToken(googleOnly.GetAccessToken())

	_, err = api.UnlinkGoogle(ctx, "anything")
	fixture.RequireAPIError(t, err, http.StatusConflict, "NO_PASSWORD")

	_, err = api.LinkGoogle(ctx, googleToken(t, "google-iam033-other-"+googleEmail, googleEmail), "anything")
	fixture.RequireAPIError(t, err, http.StatusConflict, "NO_PASSWORD")
}

// Scenario: E2E-IAM-034, Release 10.3, unlinking removes Google, revokes every
// other session and answers a new one (FR-IAM-06).
func TestUnlinkGoogleRevokesOtherSessions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "iam034")
	subject := "google-iam034-" + owner.Email

	_, err := owner.API.LinkGoogle(ctx, googleToken(t, subject, owner.Email), owner.Password)
	require.NoError(t, err)

	viaGoogle, err := fixture.Anonymous(t).SignInWithGoogleIDToken(ctx, googleToken(t, subject, owner.Email))
	require.NoError(t, err)

	fresh, err := owner.API.UnlinkGoogle(ctx, owner.Password)
	require.NoError(t, err)
	assert.False(t, fresh.GetUser().GetGoogleLinked())
	assert.True(t, fresh.GetUser().GetHasPassword())

	for name, refreshToken := range map[string]string{
		"the session of the Google sign-in": viaGoogle.GetRefreshToken(),
		"the session that unlinked":         owner.Session.GetRefreshToken(),
	} {
		_, err = fixture.Anonymous(t).Refresh(ctx, refreshToken)
		fixture.RequireAPIError(t, err, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID")
		t.Logf("%s: revoked", name)
	}

	_, err = fixture.Anonymous(t).Refresh(ctx, fresh.GetRefreshToken())
	require.NoError(t, err, "the caller keeps a session")

	_, err = fixture.Anonymous(t).Login(ctx, owner.Email, owner.Password)
	require.NoError(t, err, "the password signs in")

	// Google sign-in is off until the user links again: the e-mail match that
	// would link the same Google account again is refused.
	_, err = fixture.Anonymous(t).SignInWithGoogleIDToken(ctx, googleToken(t, subject, owner.Email))
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GOOGLE_SIGNIN_BLOCKED")

	relinked, err := withSession(t, fresh).LinkGoogle(ctx, googleToken(t, subject, owner.Email), owner.Password)
	require.NoError(t, err)
	assert.True(t, relinked.GetGoogleLinked())

	_, err = fixture.Anonymous(t).SignInWithGoogleIDToken(ctx, googleToken(t, subject, owner.Email))
	require.NoError(t, err, "linked again, Google signs in")
}

// withSession is the owner's client on the session that unlinking answered.
func withSession(t *testing.T, session *identityv1.Session) *client.Client {
	t.Helper()

	return fixture.Anonymous(t).WithToken(session.GetAccessToken())
}
