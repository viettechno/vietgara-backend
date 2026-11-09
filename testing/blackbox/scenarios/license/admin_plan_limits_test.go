// Scenario: E2E-LIC-006
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/03_License_Subscription.md
package license_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// A plan with subscribers keeps the limits they bought: an edit of a limit is
// refused (PLAN_HAS_SUBSCRIBERS), the owner's limits do not move, and the plan's
// name and availability can still change. A change of terms means a new plan.
func TestAdminCannotChangeLimitsOfSubscribedPlan(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)

	code := fixture.UniquePlanCode("qa-limit-test")
	createdPlan, err := admin.API.CreatePlan(ctx, &licensev1.Plan{
		Code: code, Name: "QA Limit Test", Price: 100000, Currency: "VND",
		BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_YEARLY,
		Limits:       &licensev1.PlanLimits{MaxGarages: 1, MaxStaff: 2, MaxTransactionsPerDay: 10},
		IsPublic:     true, IsActive: true,
	})
	require.NoError(t, err)
	fixture.RetirePlanAfterTest(t, admin, createdPlan.GetId())

	// Before anyone subscribes the limits are editable.
	edited, err := admin.API.UpdatePlan(ctx, createdPlan.GetId(), &licensev1.Plan{Limits: &licensev1.PlanLimits{MaxStaff: 3}}, "limits.max_staff")
	require.NoError(t, err)
	assert.EqualValues(t, 3, edited.GetLimits().GetMaxStaff())

	owner := fixture.NewVerifiedOwner(t, "lic-limit-owner")
	sub := fixture.SwitchPlanByID(t, owner, createdPlan.GetId(), createdPlan.GetName())
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, sub.GetStatus())
	assert.EqualValues(t, 3, sub.GetPlan().GetLimits().GetMaxStaff())

	_, err = admin.API.UpdatePlan(ctx, createdPlan.GetId(), &licensev1.Plan{Limits: &licensev1.PlanLimits{MaxStaff: 5}}, "limits.max_staff")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "PLAN_HAS_SUBSCRIBERS")

	after, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, after.GetPlan().GetLimits().GetMaxStaff(), "the owner keeps the limits bought")

	renamed, err := admin.API.UpdatePlan(ctx, createdPlan.GetId(), &licensev1.Plan{Name: "QA Limit Test 2"}, "name")
	require.NoError(t, err, "the name is not a term")
	assert.Equal(t, "QA Limit Test 2", renamed.GetName())
}
