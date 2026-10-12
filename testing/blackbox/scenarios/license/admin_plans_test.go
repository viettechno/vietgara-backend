// Scenario: E2E-LIC-005
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

func TestAdminCreatePlans(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)

	// Create QA Hidden plan
	hiddenPlan := &licensev1.Plan{
		Code:         "qa-hidden",
		Name:         "QA Hidden",
		Price:        50000,
		Currency:     "VND",
		BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_MONTHLY,
		Limits: &licensev1.PlanLimits{
			MaxGarages:            1,
			MaxStaff:              1,
			MaxTransactionsPerDay: 5,
		},
		IsPublic: false,
		IsActive: true,
	}
	createdHidden, err := admin.API.CreatePlan(ctx, hiddenPlan)
	require.NoError(t, err)
	assert.Equal(t, "qa-hidden", createdHidden.GetCode())
	assert.False(t, createdHidden.GetIsPublic())

	// Create QA Yearly plan
	yearlyPlan := &licensev1.Plan{
		Code:         "qa-yearly",
		Name:         "QA Yearly",
		Price:        5000000,
		Currency:     "VND",
		BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_YEARLY,
		Limits: &licensev1.PlanLimits{
			MaxGarages:            3,
			MaxStaff:              30,
			MaxTransactionsPerDay: 300,
		},
		IsPublic: true,
		IsActive: true,
	}
	createdYearly, err := admin.API.CreatePlan(ctx, yearlyPlan)
	require.NoError(t, err)
	assert.Equal(t, "qa-yearly", createdYearly.GetCode())
	assert.True(t, createdYearly.GetIsPublic())

	// Try to create duplicate code qa-small (assuming it exists in baseline)
	duplicatePlan := &licensev1.Plan{
		Code:         "qa-small",
		Name:         "QA Duplicate",
		Price:        1,
		Currency:     "VND",
		BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_MONTHLY,
		Limits: &licensev1.PlanLimits{
			MaxGarages:            1,
			MaxStaff:              1,
			MaxTransactionsPerDay: 1,
		},
		IsPublic: true,
		IsActive: true,
	}
	_, err = admin.API.CreatePlan(ctx, duplicatePlan)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	// Verify code field is not editable on update
	allPlans, err := admin.API.ListAllPlans(ctx)
	require.NoError(t, err)
	var yearlyID string
	for _, p := range allPlans {
		if p.GetCode() == "qa-yearly" {
			yearlyID = p.GetId()
			break
		}
	}
	require.NotEmpty(t, yearlyID)

	// Try to update the code - should be rejected or ignored
	updatedYearly, err := admin.API.UpdatePlan(ctx, yearlyID, &licensev1.Plan{Code: "qa-yearly-modified"})
	require.NoError(t, err)
	assert.Equal(t, "qa-yearly", updatedYearly.GetCode(), "plan code must be immutable")
}

func TestAdminListAllPlans(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)

	allPlans, err := admin.API.ListAllPlans(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, allPlans)

	// Should include hidden plans
	hasHidden := false
	for _, p := range allPlans {
		if !p.GetIsPublic() {
			hasHidden = true
			break
		}
	}
	assert.True(t, hasHidden, "admin list should include hidden plans")

	// Regular owner should only see public plans
	owner := fixture.NewVerifiedOwner(t, "lic-list-plans")
	publicPlans, err := owner.API.ListPlans(ctx)
	require.NoError(t, err)

	for _, p := range publicPlans {
		assert.True(t, p.GetIsPublic(), "owner list should only have public plans")
	}
}
