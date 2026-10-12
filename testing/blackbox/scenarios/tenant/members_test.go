package tenant_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-TEN-014 (partial): changing a member's groups changes
// permissions at once, as the union of groups; removing them reverts.
// Sidebar details are web-E2E.
func TestChangeMemberGroups(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "groups-owner", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Groups"})
	require.NoError(t, err)
	groups := groupIDs(t, owner.API, garage.GetId())

	member := inviteMember(t, owner.API, garage.GetId(), []string{groups["Technician"]}, "group-member")

	_, err = member.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

	row := staffByUserID(t, owner.API, garage.GetId(), member.Session.GetUser().GetId())
	changed, err := owner.API.UpdateStaff(ctx, garage.GetId(), row.GetId(),
		&tenantv1.Staff{GroupIds: []string{groups["Technician"], groups["Service Advisor"]}}, "group_ids")
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{groups["Technician"], groups["Service Advisor"]}, changed.GetGroupIds())

	phone := "0900000931"
	_, err = member.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "Khách Nhóm", Phone: &phone, PersonalDataConsent: true})
	require.NoError(t, err)

	reverted, err := owner.API.UpdateStaff(ctx, garage.GetId(), row.GetId(),
		&tenantv1.Staff{GroupIds: []string{groups["Technician"]}}, "group_ids")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{groups["Technician"]}, reverted.GetGroupIds())

	_, err = member.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")
}

// Scenario: E2E-TEN-015 (partial): deactivating a member locks them out,
// reactivating restores access, removing deletes membership; the owner's
// own row is protected. Confirm copy is web-E2E.
func TestMemberLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "lifecycle-owner", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Lifecycle"})
	require.NoError(t, err)
	groups := groupIDs(t, owner.API, garage.GetId())

	member := inviteMember(t, owner.API, garage.GetId(), []string{groups["Service Advisor"]}, "lifecycle-member")
	row := staffByUserID(t, owner.API, garage.GetId(), member.Session.GetUser().GetId())

	off, err := owner.API.UpdateStaff(ctx, garage.GetId(), row.GetId(),
		&tenantv1.Staff{Status: tenantv1.StaffStatus_STAFF_STATUS_INACTIVE}, "status")
	require.NoError(t, err)
	assert.Equal(t, tenantv1.StaffStatus_STAFF_STATUS_INACTIVE, off.GetStatus())

	_, err = member.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	on, err := owner.API.UpdateStaff(ctx, garage.GetId(), row.GetId(),
		&tenantv1.Staff{Status: tenantv1.StaffStatus_STAFF_STATUS_ACTIVE}, "status")
	require.NoError(t, err)
	assert.Equal(t, tenantv1.StaffStatus_STAFF_STATUS_ACTIVE, on.GetStatus())

	_, err = member.API.ListCustomers(ctx, garage.GetId())
	require.NoError(t, err)

	require.NoError(t, owner.API.DeleteStaff(ctx, garage.GetId(), row.GetId()))

	garages, err := member.API.ListGarages(ctx)
	require.NoError(t, err)
	assert.Empty(t, garages)

	_, err = member.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	ownRow := staffByUserID(t, owner.API, garage.GetId(), owner.Session.GetUser().GetId())
	_, err = owner.API.UpdateStaff(ctx, garage.GetId(), ownRow.GetId(),
		&tenantv1.Staff{Status: tenantv1.StaffStatus_STAFF_STATUS_INACTIVE}, "status")
	fixture.RequireAPIError(t, err, http.StatusConflict, "OWNER_MEMBERSHIP")

	err = owner.API.DeleteStaff(ctx, garage.GetId(), ownRow.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "OWNER_MEMBERSHIP")
}

// Scenario: E2E-TEN-016 (partial): a manager runs staff and garage writes
// but is refused owner-only surface (roles, garage status). Subscription
// visibility belongs to the License batch.
func TestManagerBoundaries(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "manager-owner", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Manager"})
	require.NoError(t, err)
	groups := groupIDs(t, owner.API, garage.GetId())

	manager := inviteMember(t, owner.API, garage.GetId(), []string{groups["Manager"]}, "manager16")

	_, err = manager.API.ListStaff(ctx, garage.GetId())
	require.NoError(t, err)

	// Managers invite and revoke staff.
	helper := fixture.NewVerifiedOwnerWithLocale(t, "manager-helper", identityv1.Locale_LOCALE_EN)
	cand, err := manager.API.LookupStaffCandidate(ctx, garage.GetId(), helper.Email)
	require.NoError(t, err)
	inv, err := manager.API.CreateInvitation(ctx, garage.GetId(), cand.GetUserId(), []string{groups["Technician"]})
	require.NoError(t, err)
	_, err = manager.API.RevokeInvitation(ctx, garage.GetId(), inv.GetId())
	require.NoError(t, err)

	// Managers write garage settings...
	phone := "0283000111"
	_, err = manager.API.UpdateGarage(ctx, garage.GetId(), &tenantv1.Garage{Phone: &phone}, "phone")
	require.NoError(t, err)

	// ...but garage status is owner-only. (RoleService has no owner
	// check — any signed-in user manages only their own role namespace,
	// so the “owners manage roles” rule is UI-enforced; see the PR
	// description for the follow-up.)
	_, err = manager.API.UpdateGarage(ctx, garage.GetId(),
		&tenantv1.Garage{Status: tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE}, "status")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "OWNER_ONLY")
}

// Scenario: E2E-TEN-021 (partial): a member of two garages holds separate
// permissions in each, and counts once in subscription usage.
func TestTwoGaragePermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "two-garage-owner", identityv1.Locale_LOCALE_EN)
	upgradeToPro(t, owner.API)

	gA, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Two A"})
	require.NoError(t, err)
	gB, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Two B"})
	require.NoError(t, err)

	before, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)

	groupsA := groupIDs(t, owner.API, gA.GetId())
	groupsB := groupIDs(t, owner.API, gB.GetId())
	member := inviteMember(t, owner.API, gA.GetId(), []string{groupsA["Technician"]}, "two-garage-tech")

	cand, err := owner.API.LookupStaffCandidate(ctx, gB.GetId(), member.Email)
	require.NoError(t, err)
	invB, err := owner.API.CreateInvitation(ctx, gB.GetId(), cand.GetUserId(), []string{groupsB["Service Advisor"]})
	require.NoError(t, err)
	_, err = member.API.AcceptInvitation(ctx, invB.GetId())
	require.NoError(t, err)

	_, err = member.API.ListCustomers(ctx, gA.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

	phone := "0900000941"
	_, err = member.API.CreateCustomer(ctx, gB.GetId(),
		&customerv1.Customer{FullName: "Khách B", Phone: &phone, PersonalDataConsent: true})
	require.NoError(t, err)

	rowB := staffByUserID(t, owner.API, gB.GetId(), member.Session.GetUser().GetId())
	_, err = owner.API.UpdateStaff(ctx, gB.GetId(), rowB.GetId(),
		&tenantv1.Staff{GroupIds: []string{groupsB["Technician"]}}, "group_ids")
	require.NoError(t, err)

	_, err = member.API.ListCustomers(ctx, gB.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

	after, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, before.GetUsage().GetStaff()+1, after.GetUsage().GetStaff())
}
