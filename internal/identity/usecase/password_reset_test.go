package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestPasswordReset(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)

	challenge, err := e.uc.RequestPasswordReset(t.Context(), "Owner@example.com")
	require.NoError(t, err)
	assert.Equal(t, e.now.Add(domain.OTPTTL), challenge.ExpiresAt)
	assert.Equal(t, "password_reset", e.mailer.sent[len(e.mailer.sent)-1].Tag)

	again, err := e.uc.RequestPasswordReset(t.Context(), "owner@example.com")
	require.NoError(t, err, "within the cooldown the running challenge is returned")
	assert.Equal(t, challenge, again)

	_, _, err = e.uc.VerifyPasswordResetOTP(t.Context(), "owner@example.com", "000000")
	requireReason(t, err, domain.ReasonOTPInvalid)

	token, expires, err := e.uc.VerifyPasswordResetOTP(t.Context(), "owner@example.com", e.mailer.lastCode(t, "owner@example.com"))
	require.NoError(t, err)
	assert.Equal(t, e.now.Add(domain.ResetTokenTTL), expires)

	requireReason(t, e.uc.ResetPassword(t.Context(), token, "short"), apperror.ReasonValidation)
	require.NoError(t, e.uc.ResetPassword(t.Context(), token, "brand-new-pass"))
	requireReason(t, e.uc.ResetPassword(t.Context(), token, "brand-new-pass"), domain.ReasonResetTokenInvalid)

	_, err = e.uc.Refresh(t.Context(), session.RefreshToken)
	requireReason(t, err, domain.ReasonRefreshTokenInvalid, "every session is signed out")

	_, err = e.uc.Login(t.Context(), "owner@example.com", "brand-new-pass")
	require.NoError(t, err)
	assert.True(t, e.accounts.get(session.Account.ID).EmailVerified(), "the OTP proved the e-mail")
	assert.Equal(t, []string{"account.email_verified"}, e.events.types)
}

func TestPasswordResetWithoutEnumeration(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)

	_, err := e.uc.RequestPasswordReset(t.Context(), "nobody@example.com")
	require.NoError(t, err)

	_, err = e.uc.RequestPasswordReset(t.Context(), "bad")
	requireReason(t, err, apperror.ReasonValidation)

	_, _, err = e.uc.VerifyPasswordResetOTP(t.Context(), "nobody@example.com", "123456")
	requireReason(t, err, domain.ReasonOTPExpired)
	_, _, err = e.uc.VerifyPasswordResetOTP(t.Context(), "bad", "123456")
	requireReason(t, err, domain.ReasonOTPInvalid)

	e.accounts.get(session.Account.ID).Status = domain.AccountStatusSuspended
	sent := len(e.mailer.sent)
	_, err = e.uc.RequestPasswordReset(t.Context(), "owner@example.com")
	require.NoError(t, err)
	assert.Len(t, e.mailer.sent, sent, "suspended accounts get nothing")

	e.accounts.get(session.Account.ID).Status = domain.AccountStatusActive
	e.mailer.err = errBoom
	_, err = e.uc.RequestPasswordReset(t.Context(), "owner@example.com")
	require.NoError(t, err, "delivery failures are logged, not revealed")

	e.accounts.findErr = errBoom
	_, err = e.uc.RequestPasswordReset(t.Context(), "owner@example.com")
	requireReason(t, err, apperror.ReasonInternal)
	_, _, err = e.uc.VerifyPasswordResetOTP(t.Context(), "owner@example.com", "1")
	requireReason(t, err, apperror.ReasonInternal)
}

func TestResetPasswordOnVerifiedAccount(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)
	verified, err := e.uc.VerifyEmail(t.Context(), session.Account.ID, e.mailer.lastCode(t, "owner@example.com"), "")
	require.NoError(t, err)

	require.NoError(t, e.store.SaveResetToken(t.Context(), "tok", verified.Account.ID, domain.ResetTokenTTL))
	require.NoError(t, e.uc.ResetPassword(t.Context(), "tok", "another-pass"))

	require.NoError(t, e.store.SaveResetToken(t.Context(), "tok2", verified.Account.ID, domain.ResetTokenTTL))
	e.accounts.updateErr = errBoom
	requireReason(t, e.uc.ResetPassword(t.Context(), "tok2", "another-pass"), apperror.ReasonInternal)

	require.NoError(t, e.store.SaveResetToken(t.Context(), "tok3", "gone", domain.ResetTokenTTL))
	requireReason(t, e.uc.ResetPassword(t.Context(), "tok3", "another-pass"), apperror.ReasonUnauthenticated)
}
