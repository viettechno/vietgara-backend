// Scenario: E2E-LIC-006
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/03_License_Subscription.md
package license_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestAdminChangePlanLimits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)

	// Create a test plan
	plan := &licensev1.Plan{
		Code:         "qa-limit-test",
		Name:         "QA Limit Test",
		Price:        100000,
		Currency:     "VND",
		BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_MONTHLY,
		Limits: &licensev1.PlanLimits{
			MaxGarages:            1,
			MaxStaff:              2,
			MaxTransactionsPerDay: 10,
		},
		IsPublic: true,
		IsActive: true,
	}
	createdPlan, err := admin.API.CreatePlan(ctx, plan)
	require.NoError(t, err)

	// Create an owner and subscribe to the plan
	owner := fixture.NewVerifiedOwner(t, "lic-limit-owner")
	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Limit Test"})
	require.NoError(t, err)

	sub := fixture.SwitchPlanByID(t, owner, createdPlan.GetId(), createdPlan.GetName())
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, sub.GetStatus())
	assert.Equal(t, "qa-limit-test", sub.GetPlan().GetCode())
	assert.EqualValues(t, 2, sub.GetPlan().GetLimits().GetMaxStaff(), "initial staff limit should be 2")

	// Admin changes max staff from 2 to 3
	updatedPlan, err := admin.API.UpdatePlan(ctx, createdPlan.GetId(), &licensev1.Plan{
		Limits: &licensev1.PlanLimits{MaxStaff: 3},
	}, "limits.max_staff")
	require.NoError(t, err)
	assert.EqualValues(t, 3, updatedPlan.GetLimits().GetMaxStaff())

	// Owner should see the new limit immediately
	subAfter, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, subAfter.GetPlan().GetLimits().GetMaxStaff(), "owner should see updated staff limit at once")

	// Owner can now invite a third staff member
	garageID := garage.GetId()
	groups, err := owner.API.ListStaffGroups(ctx, garageID)
	require.NoError(t, err)
	require.NotEmpty(t, groups)

	member1 := fixture.NewVerifiedOwner(t, "lic-member1")
	cand1, err := owner.API.LookupStaffCandidate(ctx, garageID, member1.Email)
	require.NoError(t, err)
	_, err = owner.API.CreateInvitation(ctx, garageID, cand1.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	member2 := fixture.NewVerifiedOwner(t, "lic-member2")
	cand2, err := owner.API.LookupStaffCandidate(ctx, garageID, member2.Email)
	require.NoError(t, err)
	_, err = owner.API.CreateInvitation(ctx, garageID, cand2.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	member3 := fixture.NewVerifiedOwner(t, "lic-member3")
	cand3, err := owner.API.LookupStaffCandidate(ctx, garageID, member3.Email)
	require.NoError(t, err)
	_, err = owner.API.CreateInvitation(ctx, garageID, cand3.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	// Third invitation should succeed now
	usage, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, usage.GetUsage().GetStaff(), "usage should reflect 3 staff (1 member + 2 pending)")

	// Admin sets max staff back to 2
	_, err = admin.API.UpdatePlan(ctx, createdPlan.GetId(), &licensev1.Plan{
		Limits: &licensev1.PlanLimits{MaxStaff: 2},
	}, "limits.max_staff")
	require.NoError(t, err)

	// Owner sees 3/2: existing staff and invitations are kept, but new ones refused
	usageAfter, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, usageAfter.GetUsage().GetStaff())

	// New invitation should be refused
	member4 := fixture.NewVerifiedOwner(t, "lic-member4")
	cand4, err := owner.API.LookupStaffCandidate(ctx, garageID, member4.Email)
	require.NoError(t, err)
	_, err = owner.API.CreateInvitation(ctx, garageID, cand4.GetUserId(), []string{groups[0].GetId()})
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusPaymentRequired, "PLAN_LIMIT_REACHED")
}
