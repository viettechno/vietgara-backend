package iam_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// waitForNewOTP polls the test inbox until an e-mail to address carries a
// code different from oldOTP (resends deliver a replacement), or fails
// after timeout. Every suite address is unique, so any new code belongs to
// the resend under test.
func waitForNewOTP(t *testing.T, mail *client.Mailpit, address, oldOTP string, timeout time.Duration) string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg, err := mail.WaitForMessage(t.Context(), address)
		require.NoError(t, err)

		otp, err := msg.OTP()
		require.NoError(t, err)

		if otp != oldOTP {
			return otp
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("no replacement code for %s within %s", address, timeout)

	return ""
}

// Scenario: E2E-IAM-004 (partial): five wrong codes lock the code out, the
// right code stays refused, and a resend replaces it. The exact copy is
// web-E2E.
func TestWrongCodesLockout(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	api := fixture.Anonymous(t)
	email := fixture.UniqueEmail(t, "iam004")

	started := time.Now()
	registered, err := api.Register(ctx, &identityv1.RegisterRequest{
		Email: email, Password: fixture.Password, FullName: "Blackbox IAM004",
	})
	require.NoError(t, err)
	authed := api.WithToken(registered.GetAccessToken())

	first, err := mail.WaitForMessage(ctx, email)
	require.NoError(t, err)
	firstOTP, err := first.OTP()
	require.NoError(t, err)

	wrong := "000000"
	if firstOTP == wrong {
		wrong = "111111"
	}

	// The first four wrong codes are refused one by one...
	for range 4 {
		_, err = authed.VerifyEmail(ctx, wrong, registered.GetRefreshToken())
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "OTP_INVALID")
	}

	// ...the fifth locks the code out, and the right code stays refused.
	_, err = authed.VerifyEmail(ctx, wrong, registered.GetRefreshToken())
	fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "OTP_TOO_MANY_ATTEMPTS")

	_, err = authed.VerifyEmail(ctx, firstOTP, registered.GetRefreshToken())
	fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "OTP_TOO_MANY_ATTEMPTS")

	// Past the resend cooldown a resend replaces the locked code.
	time.Sleep(time.Until(started.Add(35 * time.Second)))
	_, err = authed.SendVerificationOtp(ctx)
	require.NoError(t, err)

	secondOTP := waitForNewOTP(t, mail, email, firstOTP, 30*time.Second)
	_, err = authed.VerifyEmail(ctx, firstOTP, registered.GetRefreshToken())
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "OTP_INVALID")

	verified, err := authed.VerifyEmail(ctx, secondOTP, registered.GetRefreshToken())
	require.NoError(t, err)
	assert.True(t, verified.GetUser().GetEmailVerified())
}

// Scenario: E2E-IAM-006 (partial): resending inside the 30-second cooldown
// is refused; after it the resend replaces the old code. The button
// countdown is web-E2E.
func TestResendCooldown(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	api := fixture.Anonymous(t)
	email := fixture.UniqueEmail(t, "iam006")

	started := time.Now()
	registered, err := api.Register(ctx, &identityv1.RegisterRequest{
		Email: email, Password: fixture.Password, FullName: "Blackbox IAM006",
	})
	require.NoError(t, err)
	authed := api.WithToken(registered.GetAccessToken())

	first, err := mail.WaitForMessage(ctx, email)
	require.NoError(t, err)
	firstOTP, err := first.OTP()
	require.NoError(t, err)

	_, err = authed.SendVerificationOtp(ctx)
	fixture.RequireAPIError(t, err, http.StatusTooManyRequests, "OTP_RESEND_TOO_SOON")

	time.Sleep(time.Until(started.Add(35 * time.Second)))
	_, err = authed.SendVerificationOtp(ctx)
	require.NoError(t, err)

	secondOTP := waitForNewOTP(t, mail, email, firstOTP, 30*time.Second)
	_, err = authed.VerifyEmail(ctx, firstOTP, registered.GetRefreshToken())
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "OTP_INVALID")

	verified, err := authed.VerifyEmail(ctx, secondOTP, registered.GetRefreshToken())
	require.NoError(t, err)
	assert.True(t, verified.GetUser().GetEmailVerified())
}
