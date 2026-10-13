package license_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-LIC-007 (partial): a plan taken off sale is not offered to
// owners, an owner already on it keeps it, and putting it back on sale
// offers it again. The "Retired" label of the admin table is web-E2E.
func TestRetiredPlanKeepsSubscribers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)

	created, err := admin.API.CreatePlan(ctx, &licensev1.Plan{
		Code: fmt.Sprintf("qa-retire-%d", time.Now().UnixNano()), Name: "QA Retire",
		Price: 100000, Currency: "VND", BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_MONTHLY,
		Limits:   &licensev1.PlanLimits{MaxGarages: 1, MaxStaff: 2, MaxTransactionsPerDay: 10},
		IsPublic: true, IsActive: true,
	})
	require.NoError(t, err)

	subscriber := fixture.NewVerifiedOwner(t, "lic-retire-sub")
	sub := fixture.SwitchPlanByID(t, subscriber, created.GetId(), created.GetName())
	require.Equal(t, created.GetCode(), sub.GetPlan().GetCode())

	other := fixture.NewVerifiedOwner(t, "lic-retire-other")

	offered := func(owner *fixture.Actor) bool {
		plans, listErr := owner.API.ListPlans(ctx)
		require.NoError(t, listErr)

		for _, p := range plans {
			if p.GetId() == created.GetId() {
				return true
			}
		}

		return false
	}

	require.True(t, offered(other), "on sale: offered")

	_, err = admin.API.UpdatePlan(ctx, created.GetId(), &licensev1.Plan{IsActive: false}, "is_active")
	require.NoError(t, err)

	assert.False(t, offered(other), "off sale: no longer offered")

	_, err = other.API.CreatePlanPaymentRequest(ctx, created.GetId(), licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	require.Error(t, err, "an inactive plan cannot be chosen")

	kept, err := subscriber.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, created.GetId(), kept.GetPlan().GetId(), "the subscriber keeps the plan")
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, kept.GetStatus())
	assert.EqualValues(t, 2, kept.GetPlan().GetLimits().GetMaxStaff(), "with its limits")

	_, err = admin.API.UpdatePlan(ctx, created.GetId(), &licensev1.Plan{IsActive: true}, "is_active")
	require.NoError(t, err)

	assert.True(t, offered(other), "back on sale: offered again")
}
