package iam_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-IAM-011 (partial): a forgotten password is reset through
// the e-mailed code, the old password stops working, and every session is
// revoked. Page copy and navigation are web-E2E.
func TestPasswordReset(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	api := fixture.Anonymous(t)

	owner := fixture.NewVerifiedOwner(t, "reset")

	const newPassword = "Blackbox#2027"

	// A second session stands in for the second browser.
	second, err := api.Login(ctx, owner.Email, fixture.Password)
	require.NoError(t, err)

	challenge, err := api.RequestPasswordReset(ctx, owner.Email)
	require.NoError(t, err)
	require.NotNil(t, challenge.GetExpiresAt())
	assert.WithinRange(t, challenge.GetExpiresAt().AsTime(),
		time.Now().Add(90*time.Second), time.Now().Add(150*time.Second))

	// The reset e-mail is Vietnamese (the default language).
	msg, err := mail.WaitForMessage(ctx, owner.Email)
	require.NoError(t, err)
	code, err := msg.OTP()
	require.NoError(t, err)
	assert.Contains(t, msg.Subject, "Mã đặt lại mật khẩu VietGara: "+code)
	assert.Contains(t, msg.Text, "Xin chào "+owner.Session.GetUser().GetFullName())

	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}

	_, err = api.VerifyPasswordResetOtp(ctx, owner.Email, wrong)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "OTP_INVALID")

	verified, err := api.VerifyPasswordResetOtp(ctx, owner.Email, code)
	require.NoError(t, err)
	require.NotEmpty(t, verified.GetResetToken())

	require.NoError(t, api.ResetPassword(ctx, verified.GetResetToken(), newPassword))

	_, err = api.Login(ctx, owner.Email, fixture.Password)
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "INVALID_CREDENTIALS")

	renewed, err := api.Login(ctx, owner.Email, newPassword)
	require.NoError(t, err)
	assert.Equal(t, owner.Session.GetUser().GetId(), renewed.GetUser().GetId())

	// Every session was revoked: the second browser's refresh token is dead.
	_, err = api.Refresh(ctx, second.GetRefreshToken())
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "REFRESH_TOKEN_INVALID")
}

// Scenario: E2E-IAM-012 (partial): resetting for an unknown address
// behaves exactly as for a user — success, no e-mail, and the code step
// refuses. Wording equality is web-E2E.
func TestPasswordResetUnknownUser(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	api := fixture.Anonymous(t)
	email := fixture.UniqueEmail(t, "nobody")

	challenge, err := api.RequestPasswordReset(ctx, email)
	require.NoError(t, err)
	require.NotNil(t, challenge.GetExpiresAt())

	assertNoMail(t, mail, email, 6*time.Second)

	_, err = api.VerifyPasswordResetOtp(ctx, email, "123456")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "OTP_EXPIRED")
}
