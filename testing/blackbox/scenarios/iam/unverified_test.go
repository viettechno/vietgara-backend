package iam_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-IAM-002 (partial): an unverified user reaches only its
// account, verification and the public plans; garage, customer and
// subscription APIs refuse it. Page routing is web-E2E.
func TestUnverifiedCapabilities(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	api := fixture.Anonymous(t)
	email := fixture.UniqueEmail(t, "unverified")

	registered, err := api.Register(ctx, &identityv1.RegisterRequest{
		Email: email, Password: fixture.Password, FullName: "Blackbox Unverified",
	})
	require.NoError(t, err)

	unverified := api.WithToken(registered.GetAccessToken())

	// The account surface works: profile, profile edits and the public
	// plans, which the verification page needs.
	me, err := unverified.Me(ctx)
	require.NoError(t, err)
	assert.Equal(t, email, me.GetEmail())
	assert.False(t, me.GetEmailVerified())

	edited, err := unverified.UpdateMe(ctx, &identityv1.User{FullName: "Blackbox Unverified Edited"}, "full_name")
	require.NoError(t, err)
	assert.Equal(t, "Blackbox Unverified Edited", edited.GetFullName())

	_, err = unverified.ListPlans(ctx)
	require.NoError(t, err)

	// Everything else bounces back to verification.
	_, err = unverified.CreateGarage(ctx, &tenantv1.Garage{Name: "Too early"})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "EMAIL_NOT_VERIFIED")

	_, err = unverified.MySubscription(ctx)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "EMAIL_NOT_VERIFIED")

	_, err = unverified.CreateCustomer(ctx, "00000000-0000-0000-0000-000000000000",
		&customerv1.Customer{FullName: "Never created"})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "EMAIL_NOT_VERIFIED")
}

// Scenario: E2E-IAM-003 (partial): signing in unverified delivers a new
// code that verifies the user. The resend cooldown applies, so the test
// waits it out once.
func TestSignInUnverifiedSendsNewCode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	api := fixture.Anonymous(t)
	email := fixture.UniqueEmail(t, "unverified-resend")

	started := time.Now()
	_, err := api.Register(ctx, &identityv1.RegisterRequest{
		Email: email, Password: fixture.Password, FullName: "Blackbox Resend",
	})
	require.NoError(t, err)
	first, err := mail.WaitForMessage(ctx, email)
	require.NoError(t, err)
	firstOTP, err := first.OTP()
	require.NoError(t, err)

	// The signup code is still inside its resend cooldown: wait it out so
	// the sign-in resend is a new code, not a rate-limit refusal.
	time.Sleep(time.Until(started.Add(33 * time.Second)))

	session, err := api.Login(ctx, email, fixture.Password)
	require.NoError(t, err)
	assert.False(t, session.GetUser().GetEmailVerified())

	_, err = api.WithToken(session.GetAccessToken()).SendVerificationOtp(ctx)
	require.NoError(t, err)

	secondOTP := waitForNewOTP(t, mail, email, firstOTP, 30*time.Second)
	verified, err := api.WithToken(session.GetAccessToken()).VerifyEmail(ctx, secondOTP, session.GetRefreshToken())
	require.NoError(t, err)
	assert.True(t, verified.GetUser().GetEmailVerified())
}
