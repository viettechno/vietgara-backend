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

// The billing cycle of a plan can only change while no subscription uses it;
// every other field stays editable. A plan counts repair orders unless the
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

	repriced, err := admin.API.UpdatePlan(ctx, created.GetId(), &licensev1.Plan{Price: 1200000}, "price")
	require.NoError(t, err, "the price may change; it applies to the next payment")
	assert.EqualValues(t, 1200000, repriced.GetPrice())
	assert.Equal(t, licensev1.BillingCycle_BILLING_CYCLE_MONTHLY, repriced.GetBillingCycle())
}
