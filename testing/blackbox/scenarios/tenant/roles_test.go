package tenant_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-TEN-017 (partial): creating a custom role normalizes
// write permissions to include their reads; the role lists without the
// default tag and deletes cleanly. Editor counts and undo are web-E2E.
func TestCreateCustomRole(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "role-owner", identityv1.Locale_LOCALE_EN)

	created, err := owner.API.CreateRole(ctx, &tenantv1.Role{
		Name: "Blackbox Partner Clerk", Description: "Manages partners only",
		Permissions: []tenantv1.Permission{tenantv1.Permission_PERMISSION_SUPPLIER_WRITE},
	})
	require.NoError(t, err)
	assert.Contains(t, created.GetPermissions(), tenantv1.Permission_PERMISSION_SUPPLIER_WRITE)
	assert.Contains(t, created.GetPermissions(), tenantv1.Permission_PERMISSION_SUPPLIER_READ)
	assert.False(t, created.GetIsDefault())
	assert.Zero(t, created.GetGroupCount())

	roles, err := owner.API.ListRoles(ctx)
	require.NoError(t, err)

	found := false

	for _, r := range roles {
		if r.GetId() == created.GetId() {
			found = true

			assert.False(t, r.GetIsDefault())
		}
	}

	require.True(t, found, "created role listed")

	require.NoError(t, owner.API.DeleteRole(ctx, created.GetId()))

	roles, err = owner.API.ListRoles(ctx)
	require.NoError(t, err)

	for _, r := range roles {
		assert.NotEqual(t, created.GetId(), r.GetId())
	}
}

// Scenario: E2E-TEN-019 (partial): role and group names are unique
// ignoring letter case, and a role in use cannot be deleted.
func TestRoleGroupUniqueness(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "unique-owner", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Unique"})
	require.NoError(t, err)

	role, err := owner.API.CreateRole(ctx, &tenantv1.Role{Name: "Blackbox Viewer"})
	require.NoError(t, err)

	_, err = owner.API.CreateRole(ctx, &tenantv1.Role{Name: "blackbox viewer"})
	fixture.RequireAPIError(t, err, http.StatusConflict, "ROLE_NAME_TAKEN")

	group, err := owner.API.CreateStaffGroup(ctx, garage.GetId(), &tenantv1.StaffGroup{
		Name: "Blackbox Viewers", RoleId: role.GetId(),
	})
	require.NoError(t, err)

	err = owner.API.DeleteRole(ctx, role.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "ROLE_IN_USE")

	_, err = owner.API.CreateStaffGroup(ctx, garage.GetId(), &tenantv1.StaffGroup{
		Name: "BLACKBOX VIEWERS", RoleId: role.GetId(),
	})
	fixture.RequireAPIError(t, err, http.StatusConflict, "GROUP_NAME_TAKEN")

	require.NoError(t, owner.API.DeleteStaffGroup(ctx, garage.GetId(), group.GetId()))
	require.NoError(t, owner.API.DeleteRole(ctx, role.GetId()))
}

// Scenario: E2E-TEN-020 (partial): staff groups are created per garage,
// scoped to it, re-roled and deleted; a member keeps its other groups.
// The delete-confirm copy is web-E2E.
func TestStaffGroups(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "groups-owner", identityv1.Locale_LOCALE_EN)
	upgradeToPro(t, owner)

	gA, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Groups A"})
	require.NoError(t, err)
	gB, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Groups B"})
	require.NoError(t, err)

	roles, err := owner.API.ListRoles(ctx)
	require.NoError(t, err)

	roleID := map[string]string{}
	for _, r := range roles {
		roleID[r.GetName()] = r.GetId()
	}

	require.NotEmpty(t, roleID["Technician"])
	require.NotEmpty(t, roleID["Service Advisor"])

	night, err := owner.API.CreateStaffGroup(ctx, gA.GetId(), &tenantv1.StaffGroup{
		Name: "Blackbox Night Shift", RoleId: roleID["Technician"],
	})
	require.NoError(t, err)
	assert.Zero(t, night.GetMemberCount())

	inB, err := owner.API.ListStaffGroups(ctx, gB.GetId())
	require.NoError(t, err)

	for _, g := range inB {
		assert.NotEqual(t, "Blackbox Night Shift", g.GetName())
	}

	rerolled, err := owner.API.UpdateStaffGroup(ctx, gA.GetId(), night.GetId(), &tenantv1.StaffGroup{
		RoleId: roleID["Service Advisor"],
	}, "role_id")
	require.NoError(t, err)
	assert.Equal(t, roleID["Service Advisor"], rerolled.GetRoleId())

	// A member of two groups keeps the other when one is deleted.
	groups := groupIDs(t, owner.API, gA.GetId())
	member := inviteMember(t, owner.API, gA.GetId(),
		[]string{night.GetId(), groups["Technician"]}, "groups-member")

	require.NoError(t, owner.API.DeleteStaffGroup(ctx, gA.GetId(), night.GetId()))

	inA, err := owner.API.ListStaffGroups(ctx, gA.GetId())
	require.NoError(t, err)

	for _, g := range inA {
		assert.NotEqual(t, "Blackbox Night Shift", g.GetName())
	}

	row := staffByUserID(t, owner.API, gA.GetId(), member.Session.GetUser().GetId())
	assert.ElementsMatch(t, []string{groups["Technician"]}, row.GetGroupIds())
}
