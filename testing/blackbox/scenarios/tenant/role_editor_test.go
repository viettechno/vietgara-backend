package tenant_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/timeshift"
)

// Scenario: E2E-TEN-018 (partial): editing a default role changes what its
// members can do at once, and saving its default permissions again
// restores it with the name and description unchanged. The unsaved-change
// guards, undo and the leave-page warning are web-E2E.
func TestDefaultRoleEditAndReset(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "role-edit-owner", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Role Editor"})
	require.NoError(t, err)

	groups := groupIDs(t, owner.API, garage.GetId())
	member := inviteMember(t, owner.API, garage.GetId(), []string{groups["Technician"]}, "role-edit-member")

	roles, err := owner.API.ListRoles(ctx)
	require.NoError(t, err)

	var technician *tenantv1.Role

	for _, r := range roles {
		if r.GetName() == "Technician" {
			technician = r
		}
	}

	require.NotNil(t, technician, "default Technician role")
	require.True(t, technician.GetIsDefault())
	require.NotEmpty(t, technician.GetDefaultPermissions())
	assert.ElementsMatch(t, technician.GetDefaultPermissions(), technician.GetPermissions(), "unchanged: matches its defaults")

	_, err = member.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

	edited := append(append([]tenantv1.Permission{}, technician.GetPermissions()...), tenantv1.Permission_PERMISSION_CUSTOMER_READ)

	saved, err := owner.API.UpdateRole(ctx, technician.GetId(), &tenantv1.Role{Permissions: edited}, "permissions")
	require.NoError(t, err)
	assert.Contains(t, saved.GetPermissions(), tenantv1.Permission_PERMISSION_CUSTOMER_READ)

	_, err = member.API.ListCustomers(ctx, garage.GetId())
	require.NoError(t, err, "the member sees customers at once")

	reset, err := owner.API.UpdateRole(ctx, technician.GetId(),
		&tenantv1.Role{Permissions: technician.GetDefaultPermissions()}, "permissions")
	require.NoError(t, err)
	assert.ElementsMatch(t, technician.GetDefaultPermissions(), reset.GetPermissions())
	assert.Equal(t, technician.GetName(), reset.GetName())
	assert.Equal(t, technician.GetDescription(), reset.GetDescription())

	_, err = member.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")
}

// Scenario: E2E-TEN-012 (partial): an invitation past its expiry cannot be
// accepted and the invitee does not join; the owner sees it as expired
// and can invite the user again. The stored expiry is moved with the SQL
// helper H4 (stubbed target only).
func TestExpiredInvitationCannotBeAccepted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "expired-inv-owner", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Expired Invitation"})
	require.NoError(t, err)

	groups := groupIDs(t, owner.API, garage.GetId())
	invitee, inv := invitePending(t, owner.API, garage.GetId(), []string{groups["Technician"]}, "expired-inv-invitee")

	timeshift.ExpireInvitation(t, invitee.Email)

	_, err = invitee.API.AcceptInvitation(ctx, inv.GetId())
	require.Error(t, err, "accepting an expired invitation is refused")

	staff, err := owner.API.ListStaff(ctx, garage.GetId())
	require.NoError(t, err)

	for _, s := range staff {
		assert.NotEqual(t, invitee.Session.GetUser().GetId(), s.GetUserId(), "the invitee did not join")
	}

	// The owner can invite the same user again.
	require.Eventually(t, func() bool {
		cand, lookupErr := owner.API.LookupStaffCandidate(ctx, garage.GetId(), invitee.Email)
		if lookupErr != nil {
			return false
		}

		_, inviteErr := owner.API.CreateInvitation(ctx, garage.GetId(), cand.GetUserId(), []string{groups["Technician"]})

		return inviteErr == nil
	}, 10*time.Second, time.Second, "a fresh invitation can be sent")
}
