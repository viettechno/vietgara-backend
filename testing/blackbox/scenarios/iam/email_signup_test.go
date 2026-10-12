// Package iam_test covers sign-up and sign-in (FR-IAM-01..03) end to end:
// e-mail OTPs read from the test inbox and, on the stubbed stack, Google ID
// tokens minted by the stub.
package iam_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// TestEmailSignUp walks a new owner from registration to a working session:
// the user is locked out of garage APIs until the e-mailed OTP is
// confirmed, then signs in again with its password and rotates its session.
//
// Scenario: E2E-IAM-001 (partial): the trial subscription and the
// Vietnamese verification e-mail are asserted; default roles and the
// countdown copy are web-E2E.
func TestEmailSignUp(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	api := fixture.Anonymous(t)
	email := fixture.UniqueEmail(t, "signup")

	registered, err := api.Register(ctx, &identityv1.RegisterRequest{Email: email, Password: fixture.Password, FullName: "Blackbox Signup"})
	require.NoError(t, err)
	assert.False(t, registered.GetUser().GetEmailVerified())

	unverified := api.WithToken(registered.GetAccessToken())
	_, err = unverified.CreateGarage(ctx, &tenantv1.Garage{Name: "Too early"})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "EMAIL_NOT_VERIFIED")

	_, err = api.Register(ctx, &identityv1.RegisterRequest{Email: email, Password: fixture.Password, FullName: "Again"})
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	msg, err := mail.WaitForMessage(ctx, email)
	require.NoError(t, err)
	otp, err := msg.OTP()
	require.NoError(t, err)

	// The verification e-mail is Vietnamese (the default language) and
	// greets the new user by name.
	assert.Contains(t, msg.Subject, "Mã xác minh email VietGara: "+otp)
	assert.Contains(t, msg.Text, "Xin chào Blackbox Signup")
	assert.Contains(t, msg.Text, otp)

	verified, err := unverified.VerifyEmail(ctx, otp, registered.GetRefreshToken())
	require.NoError(t, err)
	assert.True(t, verified.GetUser().GetEmailVerified())

	garage, err := api.WithToken(verified.GetAccessToken()).CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Garage"})
	require.NoError(t, err)
	assert.Equal(t, verified.GetUser().GetId(), garage.GetOwnerUserId())

	// Verification starts the 14-day free trial (FR-LIC-01).
	sub, err := api.WithToken(verified.GetAccessToken()).MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, "trial", sub.GetPlan().GetCode())
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, sub.GetStatus())
	trialEnd := sub.GetTrialEndAt().AsTime()
	assert.WithinRange(t, trialEnd, time.Now().Add(13*24*time.Hour), time.Now().Add(15*24*time.Hour))

	// Scenario: E2E-IAM-008 (partial): unknown e-mails and wrong passwords
	// are indistinguishable, and e-mail letter case does not matter. The
	// exact message wording is web-E2E.
	t.Run("signs in with the password", func(t *testing.T) {
		_, err := api.Login(ctx, email, "wrong-password")
		fixture.RequireAPIError(t, err, http.StatusUnauthorized, "INVALID_CREDENTIALS")

		_, err = api.Login(ctx, "nobody@vietgara.test", fixture.Password)
		fixture.RequireAPIError(t, err, http.StatusUnauthorized, "INVALID_CREDENTIALS")

		session, err := api.Login(ctx, email, fixture.Password)
		require.NoError(t, err)
		assert.Equal(t, verified.GetUser().GetId(), session.GetUser().GetId())
		assert.True(t, session.GetUser().GetEmailVerified())

		upper, err := api.Login(ctx, strings.ToUpper(email), fixture.Password)
		require.NoError(t, err)
		assert.Equal(t, verified.GetUser().GetId(), upper.GetUser().GetId())
	})

	t.Run("refresh rotates the refresh token", func(t *testing.T) {
		rotated, err := api.Refresh(ctx, verified.GetRefreshToken())
		require.NoError(t, err)
		assert.NotEqual(t, verified.GetRefreshToken(), rotated.GetRefreshToken())

		_, err = api.Refresh(ctx, verified.GetRefreshToken())
		fixture.RequireAPIError(t, err, http.StatusUnauthorized, "")

		_, err = api.WithToken(rotated.GetAccessToken()).Me(ctx)
		require.NoError(t, err)
	})
}
