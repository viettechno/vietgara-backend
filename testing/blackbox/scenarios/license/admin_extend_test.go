// Scenario: E2E-LIC-008
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/03_License_Subscription.md
package license_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestAdminFindAndExtendSubscription(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)

	// Create an owner with trial
	owner := fixture.NewVerifiedOwner(t, "lic-extend-owner")
	subBefore, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, subBefore.GetStatus())

	oldTrialEnd := subBefore.GetTrialEndAt().AsTime()
	require.NotZero(t, oldTrialEnd)

	// Admin searches by owner email
	subscriptions, err := admin.API.ListSubscriptions(ctx, owner.Email, 0)
	require.NoError(t, err)
	require.NotEmpty(t, subscriptions, "admin should find owner by email")
	// SubscriptionSummary has a nested subscription field
	assert.Equal(t, "trial", subscriptions[0].GetSubscription().GetPlan().GetCode())
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, subscriptions[0].GetSubscription().GetStatus())

	// Filter by Trial status
	trialSubs, err := admin.API.ListSubscriptions(ctx, "", licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL)
	require.NoError(t, err)
	require.NotEmpty(t, trialSubs)

	// Extend subscription by 30 days
	subscriptionID := subscriptions[0].GetSubscription().GetId()
	require.NotEmpty(t, subscriptionID)

	// Invalid extensions: 0 and 400 days
	_, err = admin.API.ExtendSubscription(ctx, subscriptionID, 0)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	_, err = admin.API.ExtendSubscription(ctx, subscriptionID, 400)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Valid extension: 30 days
	extendedSub, err := admin.API.ExtendSubscription(ctx, subscriptionID, 30)
	require.NoError(t, err)
	// ExtendSubscriptionResponse returns the updated subscription directly
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, extendedSub.GetStatus())

	// Verify owner sees extended trial
	subAfter, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, subAfter.GetStatus())

	newTrialEnd := subAfter.GetTrialEndAt().AsTime()
	diff := newTrialEnd.Sub(oldTrialEnd)
	assert.GreaterOrEqual(t, diff, 29*24*time.Hour, "trial end should move by at least 29 days")

	// Admin list should show updated subscription
	updatedSubs, err := admin.API.ListSubscriptions(ctx, owner.Email, 0)
	require.NoError(t, err)
	// The extended subscription has updated trial_end_at
	assert.True(t, updatedSubs[0].GetSubscription().GetTrialEndAt().AsTime().After(oldTrialEnd))
}
