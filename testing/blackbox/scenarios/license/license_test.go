// Package license_test covers License & Subscription at API level: the
// trial a new owner gets, switching to a paid plan, a refused downgrade
// while usage exceeds the new plan, staff-limit counting, and the garage
// entitlements read. Plan banners, the admin web and time-driven
// read-only/suspended transitions are web-E2E or need a platform admin
// and time control the API suite does not have.
package license_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// planIDByName returns the id of the plan with the given name.
func planIDByName(t *testing.T, api *client.Client, name string) string {
	t.Helper()

	plans, err := api.ListPlans(t.Context())
	require.NoError(t, err)

	for _, p := range plans {
		if p.GetName() == name {
			return p.GetId()
		}
	}

	t.Fatalf("no %q plan among the listed plans", name)

	return ""
}

// Scenario: E2E-LIC-001 (partial): a new owner gets a 14-day trial with
// trial limits — the API substance (plan, status, usage) is asserted;
// the banner and plan cards are web-E2E.
func TestNewOwnerTrial(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-trial")

	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, sub.GetStatus())
	assert.Equal(t, "trial", sub.GetPlan().GetCode())
	assert.EqualValues(t, 0, sub.GetUsage().GetGarages())
	assert.EqualValues(t, 0, sub.GetUsage().GetStaff())
	assert.EqualValues(t, 0, sub.GetUsage().GetTransactionsToday())
	assert.NotNil(t, sub.GetTrialEndAt())

	plans, err := owner.API.ListPlans(ctx)
	require.NoError(t, err)

	var names []string

	for _, p := range plans {
		names = append(names, p.GetName())
		assert.NotEqual(t, "trial", p.GetCode(), "the trial plan is not offered")
	}

	assert.Contains(t, names, "Starter")
	assert.Contains(t, names, "Pro")
}

// Scenario: E2E-LIC-001 (partial): the garage entitlements read reflects
// the owner's trial — asserted at API level; the UI lock is web-E2E.
func TestGarageEntitlementsTrial(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-entitle")

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Entitlements"})
	require.NoError(t, err)

	ent, err := owner.API.GetGarageEntitlements(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, "trial", ent.GetPlanCode())
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, ent.GetStatus())
	assert.False(t, ent.GetReadOnly())

	_, err = fixture.Anonymous(t).GetGarageEntitlements(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "")
}

// Scenario: E2E-LIC-002 (partial): the owner switches from the trial to
// a paid plan through the bank-transfer flow — the API substance (plan,
// active status) is asserted; the dialog copy and the admin list are
// web-E2E.
func TestSwitchToPaidPlan(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-switch")

	sub := fixture.SwitchPlan(t, owner, "Starter")
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, sub.GetStatus())
	assert.Equal(t, "starter", sub.GetPlan().GetCode())
	assert.NotNil(t, sub.GetCurrentPeriodEnd())

	again, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, "starter", again.GetPlan().GetCode())
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, again.GetStatus())
}

// Scenario: E2E-LIC-002 (partial): paying for an unknown or unavailable
// plan is refused — the API guard behind the plan cards.
func TestChangeToUnknownPlanRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-badplan")

	_, err := owner.API.CreatePlanPaymentRequest(ctx, "00000000-0000-4000-8000-000000000099",
		licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")
}

// Scenario: E2E-LIC-003 (partial): a downgrade is refused while usage
// exceeds the new plan — asserted at API level with two active garages
// on Pro and a refused switch to Starter (402 PLAN_LIMIT_REACHED); the
// settings-screen flow is web-E2E.
func TestDowngradeRefusedOverLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-downgrade")

	fixture.SwitchPlan(t, owner, "Pro")

	_, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Downgrade A"})
	require.NoError(t, err)

	_, err = owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Downgrade B"})
	require.NoError(t, err)

	_, err = owner.API.CreatePlanPaymentRequest(ctx, planIDByName(t, owner.API, "Starter"),
		licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	fixture.RequireAPIError(t, err, http.StatusPaymentRequired, "PLAN_LIMIT_REACHED")

	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, "professional", sub.GetPlan().GetCode())
	assert.EqualValues(t, 2, sub.GetUsage().GetGarages())
}

// Scenario: E2E-LIC-004 (partial): the staff limit counts members and
// pending invitations once per person, never the owner — asserted at API
// level through usage; the Staff page badge and the refused-invite dialog
// are web-E2E.
func TestStaffLimitCounting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "lic-staff")

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Staff Count"})
	require.NoError(t, err)

	groups, err := owner.API.ListStaffGroups(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, groups)

	member := fixture.NewVerifiedOwner(t, "lic-member")

	cand, err := owner.API.LookupStaffCandidate(ctx, garage.GetId(), member.Email)
	require.NoError(t, err)

	_, err = owner.API.CreateInvitation(ctx, garage.GetId(), cand.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	pending := fixture.NewVerifiedOwner(t, "lic-pending")

	pendingCand, err := owner.API.LookupStaffCandidate(ctx, garage.GetId(), pending.Email)
	require.NoError(t, err)

	_, err = owner.API.CreateInvitation(ctx, garage.GetId(), pendingCand.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	mine, err := member.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, mine)

	_, err = member.API.AcceptInvitation(ctx, mine[0].GetId())
	require.NoError(t, err)

	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 2, sub.GetUsage().GetStaff(), "one member plus one pending invitation, owner never counted")
}
