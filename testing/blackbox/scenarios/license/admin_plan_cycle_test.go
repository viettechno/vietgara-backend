// Scenario: E2E-LIC-021
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

// The commercial terms of a plan (here the billing cycle and the price) can
// only change while no subscription uses it; the name and description stay editable. A plan counts repair orders unless the
// admin says otherwise.
func TestPlanCycleIsLockedWhileSubscribed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)

	created, err := admin.API.CreatePlan(ctx, &licensev1.Plan{
		Code: fixture.UniquePlanCode("qa-cycle"), Name: "QA Cycle", Price: 1000000, Currency: "VND",
		BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_YEARLY,
		Limits:       &licensev1.PlanLimits{MaxGarages: 1, MaxStaff: 2, MaxTransactionsPerDay: 10},
		IsPublic:     true, IsActive: true,
	})
	require.NoError(t, err)
	fixture.RetirePlanAfterTest(t, admin, created.GetId())

	assert.Equal(t, []licensev1.TransactionKind{licensev1.TransactionKind_TRANSACTION_KIND_REPAIR_ORDER},
		created.GetCountedTransactionKinds(), "repair orders count by default")

	monthly, err := admin.API.UpdatePlan(ctx, created.GetId(),
		&licensev1.Plan{BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_MONTHLY}, "billing_cycle")
	require.NoError(t, err, "nobody uses the plan yet")
	assert.Equal(t, licensev1.BillingCycle_BILLING_CYCLE_MONTHLY, monthly.GetBillingCycle())

	subscriber := fixture.NewVerifiedOwner(t, "lic-cycle-sub")
	fixture.SwitchPlanByID(t, subscriber, created.GetId(), created.GetName())

	_, err = admin.API.UpdatePlan(ctx, created.GetId(),
		&licensev1.Plan{BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_YEARLY}, "billing_cycle")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "PLAN_HAS_SUBSCRIBERS")

	_, err = admin.API.UpdatePlan(ctx, created.GetId(), &licensev1.Plan{Price: 1200000}, "price")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "PLAN_HAS_SUBSCRIBERS")

	// A form that sends the same price and cycle back changes nothing: allowed.
	same, err := admin.API.UpdatePlan(ctx, created.GetId(),
		&licensev1.Plan{Price: 1000000, BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_MONTHLY, Description: "same terms"},
		"price", "billing_cycle", "description")
	require.NoError(t, err)
	assert.Equal(t, "same terms", same.GetDescription())
}
