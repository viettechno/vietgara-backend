package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestVerifyEmail(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)
	id := session.User.ID

	_, err := e.uc.SendEmailVerificationOTP(t.Context(), id)
	requireReason(t, err, domain.ReasonOTPResendTooSoon)

	_, err = e.uc.VerifyEmail(t.Context(), id, "000000", session.RefreshToken)
	requireReason(t, err, domain.ReasonOTPInvalid)

	code := e.mailer.lastCode(t, "owner@example.com")
	verified, err := e.uc.VerifyEmail(t.Context(), id, code, session.RefreshToken)
	require.NoError(t, err)
	assert.True(t, verified.User.EmailVerified())
	assert.Equal(t, []string{usecase.EventUserEmailVerified}, e.events.types)

	claims, err := e.issuer.Verify(verified.AccessToken)
	require.NoError(t, err)
	assert.True(t, claims.EmailVerified)

	_, err = e.uc.Refresh(t.Context(), session.RefreshToken)
	requireReason(t, err, domain.ReasonRefreshTokenInvalid)

	_, err = e.uc.SendEmailVerificationOTP(t.Context(), id)
	requireReason(t, err, domain.ReasonEmailAlreadyVerified)
	_, err = e.uc.VerifyEmail(t.Context(), id, code, "")
	requireReason(t, err, domain.ReasonEmailAlreadyVerified)
}

func TestOTPLifecycle(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)
	id := session.User.ID

	e.advance(domain.OTPResendCooldown)
	challenge, err := e.uc.SendEmailVerificationOTP(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, e.clock.Add(domain.OTPTTL), challenge.ExpiresAt)

	e.advance(domain.OTPTTL)
	_, err = e.uc.VerifyEmail(t.Context(), id, e.mailer.lastCode(t, "owner@example.com"), "")
	requireReason(t, err, domain.ReasonOTPExpired)

	_, err = e.uc.VerifyEmail(t.Context(), id, "123456", "")
	requireReason(t, err, domain.ReasonOTPExpired, "consumed")

	_, err = e.uc.SendEmailVerificationOTP(t.Context(), id)
	require.NoError(t, err)

	for range domain.OTPMaxAttempts - 1 {
		_, err = e.uc.VerifyEmail(t.Context(), id, "999999", "")
		requireReason(t, err, domain.ReasonOTPInvalid)
	}

	_, err = e.uc.VerifyEmail(t.Context(), id, "999999", "")
	requireReason(t, err, domain.ReasonOTPTooManyAttempts)

	_, err = e.uc.VerifyEmail(t.Context(), id, e.mailer.lastCode(t, "owner@example.com"), "")
	requireReason(t, err, domain.ReasonOTPTooManyAttempts)
}

func TestVerificationFailures(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)
	id := session.User.ID

	_, err := e.uc.SendEmailVerificationOTP(t.Context(), "missing")
	requireReason(t, err, apperror.ReasonUnauthenticated)
	_, err = e.uc.VerifyEmail(t.Context(), "missing", "1", "")
	requireReason(t, err, apperror.ReasonUnauthenticated)

	e.advance(time.Minute)
	e.mailer.err = errBoom
	_, err = e.uc.SendEmailVerificationOTP(t.Context(), id)
	requireReason(t, err, apperror.ReasonInternal)

	e.mailer.err = nil
	_, err = e.uc.SendEmailVerificationOTP(t.Context(), id)
	require.NoError(t, err)

	e.users.updateErr = errBoom
	_, err = e.uc.VerifyEmail(t.Context(), id, e.mailer.lastCode(t, "owner@example.com"), "")
	requireReason(t, err, apperror.ReasonInternal)

	e.users.findErr = errBoom
	_, err = e.uc.GetMe(t.Context(), id)
	requireReason(t, err, apperror.ReasonInternal)
}
