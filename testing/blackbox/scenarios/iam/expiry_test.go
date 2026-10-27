package iam_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-IAM-005 (partial): a verification code stops working after
// its two minutes, and a resent code verifies the user. It waits for real
// time (BLACKBOX_SLOW=1); the countdown text is web-E2E.
func TestVerificationCodeExpires(t *testing.T) {
	t.Parallel()
	env.RequireSlow(t, "waits the 2 minute code lifetime")

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	api := fixture.Anonymous(t)
	email := fixture.UniqueEmail(t, "iam005")

	registered, err := api.Register(ctx, &identityv1.RegisterRequest{
		Email: email, Password: fixture.Password, FullName: "Blackbox IAM005",
	})
	require.NoError(t, err)

	authed := api.WithToken(registered.GetAccessToken())

	first, err := mail.WaitForMessage(ctx, email)
	require.NoError(t, err)
	expired, err := first.OTP()
	require.NoError(t, err)

	time.Sleep(2*time.Minute + 5*time.Second)

	_, err = authed.VerifyEmail(ctx, expired, registered.GetRefreshToken())
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	_, err = authed.SendVerificationOtp(ctx)
	require.NoError(t, err)

	fresh := waitForNewOTP(t, mail, email, expired, 30*time.Second)

	verified, err := authed.VerifyEmail(ctx, fresh, registered.GetRefreshToken())
	require.NoError(t, err)
	assert.True(t, verified.GetUser().GetEmailVerified())
}

// Scenario: E2E-IAM-010 (partial): a session survives while its refresh
// token is used: the access token is renewed without signing in again.
// The 20 minute idle wait and the reload are web-E2E.
func TestSessionRefresh(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "iam010")
	api := fixture.Anonymous(t)

	session, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)
	require.NotEmpty(t, session.GetRefreshToken())

	_, err = api.WithToken(session.GetAccessToken()).Me(ctx)
	require.NoError(t, err, "the access token works")

	renewed, err := api.Refresh(ctx, session.GetRefreshToken())
	require.NoError(t, err)
	assert.NotEmpty(t, renewed.GetAccessToken())
	assert.Equal(t, owner.Email, renewed.GetUser().GetEmail())

	me, err := api.WithToken(renewed.GetAccessToken()).Me(ctx)
	require.NoError(t, err, "the renewed access token works without signing in again")
	assert.Equal(t, owner.Email, me.GetEmail())

	_, err = api.Refresh(ctx, "not-a-refresh-token")
	require.Error(t, err, "an unknown refresh token is refused")
}

// Scenario: E2E-IAM-013 (partial): the reset session lasts ten minutes;
// after that the reset token is refused and the password is unchanged.
// It waits for real time (BLACKBOX_SLOW=1); the "passwords do not match"
// check is client-side and web-E2E.
func TestResetSessionExpires(t *testing.T) {
	t.Parallel()
	env.RequireSlow(t, "waits the 10 minute reset session")

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	api := fixture.Anonymous(t)
	owner := fixture.NewVerifiedOwner(t, "iam013")

	_, err := api.RequestPasswordReset(ctx, owner.Email)
	require.NoError(t, err)

	msg, err := mail.WaitForMatching(ctx, owner.Email, isPasswordResetMail)
	require.NoError(t, err)

	code, err := msg.OTP()
	require.NoError(t, err)

	verified, err := api.VerifyPasswordResetOtp(ctx, owner.Email, code)
	require.NoError(t, err)

	time.Sleep(10*time.Minute + 10*time.Second)

	err = api.ResetPassword(ctx, verified.GetResetToken(), "Blackbox#2028")
	require.Error(t, err, "the reset session expired")

	_, err = api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err, "the password is unchanged")
}
