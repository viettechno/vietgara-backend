package iam_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/stubs/google/idtoken"
)

// Scenario: E2E-IAM-022: a user turns MFA on with an authenticator app; the
// next sign-in answers a challenge instead of tokens, and the TOTP code
// completes it. Refreshing the session does not ask again. The QR code and
// the second-step screen are web and app E2E.
func TestMfaEnrolmentAndSignIn(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "mfa-signin")
	api := fixture.Anonymous(t)

	status, err := owner.API.MfaStatus(ctx)
	require.NoError(t, err)
	assert.False(t, status.GetEnabled())

	enrollment, err := owner.API.EnrollMfa(ctx)
	require.NoError(t, err)
	assert.Contains(t, enrollment.GetOtpauthUri(), "otpauth://totp/VietGara:")
	assert.Contains(t, enrollment.GetOtpauthUri(), "secret="+enrollment.GetSecret())

	// A pending enrolment does not change sign-in.
	plain, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)
	assert.False(t, plain.GetMfaRequired())
	assert.NotEmpty(t, plain.GetAccessToken())

	app := fixture.NewAuthenticator(t, enrollment.GetSecret())
	confirmed, err := owner.API.ConfirmMfaEnrollment(ctx, app.Code(t))
	require.NoError(t, err)
	assert.Len(t, confirmed.GetRecoveryCodes(), 10)

	status, err = owner.API.MfaStatus(ctx)
	require.NoError(t, err)
	assert.True(t, status.GetEnabled())
	assert.EqualValues(t, 10, status.GetRecoveryCodesRemaining())

	me, err := owner.API.Me(ctx)
	require.NoError(t, err)
	assert.True(t, me.GetMfaEnabled())

	challenge, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)
	assert.True(t, challenge.GetMfaRequired())
	assert.NotEmpty(t, challenge.GetChallengeToken())
	assert.Empty(t, challenge.GetAccessToken())
	assert.Empty(t, challenge.GetRefreshToken())

	session, err := api.VerifyMfaChallenge(ctx, challenge.GetChallengeToken(), app.Code(t))
	require.NoError(t, err)
	assert.NotEmpty(t, session.GetAccessToken())
	assert.False(t, session.GetMfaRequired())

	_, err = api.Refresh(ctx, session.GetRefreshToken())
	require.NoError(t, err, "a refresh does not ask for the second factor again")

	_, err = api.VerifyMfaChallenge(ctx, challenge.GetChallengeToken(), "000000")
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "MFA_CHALLENGE_INVALID")
}

// Scenario: E2E-IAM-023: a recovery code signs in once, in any letter case;
// the other codes keep working.
func TestMfaRecoveryCodeWorksOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "mfa-recovery")
	api := fixture.Anonymous(t)
	_, codes := fixture.EnableMfa(t, owner)

	signIn := func(code string) error {
		challenge, err := api.Login(ctx, owner.Email, fixture.Password)
		require.NoError(t, err)

		_, err = api.VerifyMfaChallenge(ctx, challenge.GetChallengeToken(), code)

		return err
	}

	require.NoError(t, signIn(strings.ToLower(codes[0])))
	fixture.RequireAPIError(t, signIn(codes[0]), http.StatusUnprocessableEntity, "MFA_CODE_INVALID")
	require.NoError(t, signIn(codes[1]))

	status, err := owner.API.MfaStatus(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 8, status.GetRecoveryCodesRemaining())
}

// Scenario: E2E-IAM-024: five wrong codes lock the second step for 15
// minutes, whatever challenge they were sent to; the right code is refused
// while locked.
func TestMfaLocksAfterWrongCodes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "mfa-lock")
	api := fixture.Anonymous(t)
	app, _ := fixture.EnableMfa(t, owner)

	challenge, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)

	for range 4 {
		_, err = api.VerifyMfaChallenge(ctx, challenge.GetChallengeToken(), "000000")
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "MFA_CODE_INVALID")
	}

	_, err = api.VerifyMfaChallenge(ctx, challenge.GetChallengeToken(), "000000")
	fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "MFA_LOCKED")

	_, err = api.VerifyMfaChallenge(ctx, challenge.GetChallengeToken(), app.Code(t))
	fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "MFA_LOCKED")

	fresh, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err, "the password step itself is not locked")

	// The lock refused the code above without spending it; it is still a
	// valid code, so only the lock can be what refuses it now.
	_, err = api.VerifyMfaChallenge(ctx, fresh.GetChallengeToken(), app.Stale())
	fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "MFA_LOCKED")
}

// Scenario: E2E-IAM-025: recovery codes can be regenerated with a TOTP code
// (the old set stops working), and MFA is turned off with a recovery code;
// sign-in then needs no second step.
func TestMfaRegenerateAndDisable(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "mfa-disable")
	api := fixture.Anonymous(t)
	app, old := fixture.EnableMfa(t, owner)

	_, err := owner.API.RegenerateRecoveryCodes(ctx, old[0])
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "MFA_CODE_INVALID")

	fresh, err := owner.API.RegenerateRecoveryCodes(ctx, app.Code(t))
	require.NoError(t, err)
	require.Len(t, fresh.GetRecoveryCodes(), 10)

	challenge, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)

	_, err = api.VerifyMfaChallenge(ctx, challenge.GetChallengeToken(), old[1])
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "MFA_CODE_INVALID")

	require.NoError(t, owner.API.DisableMfa(ctx, fresh.GetRecoveryCodes()[0]))

	signedIn, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)
	assert.False(t, signedIn.GetMfaRequired())
	assert.NotEmpty(t, signedIn.GetAccessToken())

	status, err := owner.API.MfaStatus(ctx)
	require.NoError(t, err)
	assert.False(t, status.GetEnabled())

	fixture.RequireAPIError(t, owner.API.DisableMfa(ctx, "000000"), http.StatusConflict, "MFA_NOT_ENABLED")
}

// Scenario: E2E-IAM-026: the enrolment rules. A wrong confirming code does
// not turn MFA on, a code works once, MFA cannot be enrolled twice, and a
// Google-only user (no password to protect) cannot enrol.
func TestMfaEnrolmentRules(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "mfa-rules")
	api := fixture.Anonymous(t)

	_, err := owner.API.ConfirmMfaEnrollment(ctx, "123456")
	fixture.RequireAPIError(t, err, http.StatusConflict, "MFA_ENROLLMENT_NOT_STARTED")

	enrollment, err := owner.API.EnrollMfa(ctx)
	require.NoError(t, err)

	_, err = owner.API.ConfirmMfaEnrollment(ctx, "000000")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "MFA_CODE_INVALID")

	status, err := owner.API.MfaStatus(ctx)
	require.NoError(t, err)
	assert.False(t, status.GetEnabled())

	app := fixture.NewAuthenticator(t, enrollment.GetSecret())
	_, err = owner.API.ConfirmMfaEnrollment(ctx, app.Code(t))
	require.NoError(t, err)

	_, err = owner.API.EnrollMfa(ctx)
	fixture.RequireAPIError(t, err, http.StatusConflict, "MFA_ALREADY_ENABLED")

	challenge, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)

	_, err = api.VerifyMfaChallenge(ctx, challenge.GetChallengeToken(), app.Stale())
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "MFA_CODE_INVALID")

	t.Run("a Google-only user cannot enrol", func(t *testing.T) {
		email := fixture.UniqueEmail(t, "mfa-google")
		token, err := fixture.GoogleStub(t).IDToken(ctx, idtoken.Claims{
			Subject: "google-" + email, Email: email, EmailVerified: true, Name: "Blackbox Google", Audience: env.Get(t).GoogleClientID,
		})
		require.NoError(t, err)

		session, err := api.SignInWithGoogleIDToken(ctx, token)
		require.NoError(t, err)

		_, err = api.WithToken(session.GetAccessToken()).EnrollMfa(ctx)
		fixture.RequireAPIError(t, err, http.StatusConflict, "MFA_NOT_AVAILABLE")
	})
}

// Scenario: E2E-IAM-027: signing in with Google never asks for the second
// factor, even for a user whose e-mail account turned MFA on: Google's own
// 2-step verification protects that sign-in.
func TestMfaDoesNotApplyToGoogleSignIn(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "mfa-google-link")
	api := fixture.Anonymous(t)
	fixture.EnableMfa(t, owner)

	token, err := fixture.GoogleStub(t).IDToken(ctx, idtoken.Claims{
		Subject: "google-" + owner.Email, Email: owner.Email, EmailVerified: true, Audience: env.Get(t).GoogleClientID,
	})
	require.NoError(t, err)

	session, err := api.SignInWithGoogleIDToken(ctx, token)
	require.NoError(t, err)
	assert.False(t, session.GetMfaRequired())
	assert.NotEmpty(t, session.GetAccessToken())
	assert.Equal(t, owner.Session.GetUser().GetId(), session.GetUser().GetId())
}
