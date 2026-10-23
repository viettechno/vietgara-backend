// Package platform_test holds the scenarios of platform staff and roles
// (Release 8.1): admin staff get platform permissions through groups, and
// the API checks them on every request.
package platform_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	platformv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/platform/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

const (
	planRead          = platformv1.PlatformPermission_PLATFORM_PERMISSION_PLAN_READ
	planWrite         = platformv1.PlatformPermission_PLATFORM_PERMISSION_PLAN_WRITE
	subscriptionRead  = platformv1.PlatformPermission_PLATFORM_PERMISSION_SUBSCRIPTION_READ
	subscriptionWrite = platformv1.PlatformPermission_PLATFORM_PERMISSION_SUBSCRIPTION_WRITE
	staffRead         = platformv1.PlatformPermission_PLATFORM_PERMISSION_PLATFORM_STAFF_READ
	staffWrite        = platformv1.PlatformPermission_PLATFORM_PERMISSION_PLATFORM_STAFF_WRITE
	auditRead         = platformv1.PlatformPermission_PLATFORM_PERMISSION_AUDIT_READ
)

// uniqueName keeps roles and groups of parallel scenarios (and of earlier
// runs on a long-lived target) apart: names are unique on the platform.
func uniqueName(label string) string {
	return fmt.Sprintf("QA %s %d", label, time.Now().UnixNano())
}

// team is a role and a group holding permissions, removed at the end.
type team struct {
	role  *platformv1.PlatformRole
	group *platformv1.PlatformStaffGroup
}

func newTeam(t *testing.T, label string, permissions ...platformv1.PlatformPermission) *team {
	t.Helper()

	admin := fixture.PlatformAdmin(t)

	role, err := admin.API.CreatePlatformRole(t.Context(), &platformv1.PlatformRole{Name: uniqueName(label), Permissions: permissions})
	require.NoError(t, err)

	group, err := admin.API.CreatePlatformGroup(t.Context(), &platformv1.PlatformStaffGroup{Name: uniqueName(label), RoleId: role.GetId()})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = admin.API.DeletePlatformGroup(t.Context(), group.GetId())
		_ = admin.API.DeletePlatformRole(t.Context(), role.GetId())
	})

	return &team{role: role, group: group}
}

// join invites user into the team's group as admin and accepts as the user.
func (tm *team) join(t *testing.T, admin, user *fixture.Actor) {
	t.Helper()

	inv, err := admin.API.CreatePlatformInvitation(t.Context(), user.Email, tm.group.GetId())
	require.NoError(t, err)

	_, err = user.API.AcceptPlatformInvitation(t.Context(), inv.GetId())
	require.NoError(t, err)
}

func hasPermission(access *platformv1.MyPlatformAccess, p platformv1.PlatformPermission) bool {
	return slices.Contains(access.GetPermissions(), p)
}

// Scenario: E2E-PLT-001: the Definition of Done of Release 8.1. A platform
// admin creates the group "Billing support" with a role that has
// subscription.read only and adds a user through an invitation; the user
// signs in, reads the subscriptions but not the plans, an edit is refused,
// and removing the user from the group blocks the next request.
func TestBillingSupportSeesSubscriptionsOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	support := fixture.NewVerifiedOwner(t, "plt001")

	tm := newTeam(t, "billing", subscriptionRead)

	before, err := support.API.GetMyPlatformAccess(ctx)
	require.NoError(t, err)
	assert.Empty(t, before.GetPermissions(), "a user nobody invited has no platform access")
	assert.False(t, before.GetPlatformAdmin())

	_, err = support.API.ListSubscriptions(ctx, "", 0)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	inv, err := admin.API.CreatePlatformInvitation(ctx, support.Email, tm.group.GetId())
	require.NoError(t, err)
	assert.Equal(t, platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_PENDING, inv.GetStatus())

	// The invitation was e-mailed to the user.
	message, err := fixture.Mailpit(t).WaitForMatching(ctx, support.Email, func(m *client.Message) bool {
		return strings.Contains(m.Text, "/invitations/"+inv.GetId())
	})
	require.NoError(t, err)
	assert.Contains(t, message.Text, tm.group.GetName())

	pending, err := support.API.GetMyPlatformAccess(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, pending.GetPendingInvitationCount())

	mine, err := support.API.ListMyPlatformInvitations(ctx)
	require.NoError(t, err)
	require.Len(t, mine, 1)

	accepted, err := support.API.AcceptPlatformInvitation(ctx, inv.GetId())
	require.NoError(t, err)
	assert.Equal(t, platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_ACCEPTED, accepted.GetStatus())

	access, err := support.API.GetMyPlatformAccess(ctx)
	require.NoError(t, err)
	assert.Equal(t, []platformv1.PlatformPermission{subscriptionRead}, access.GetPermissions())

	// The same token reads the subscriptions at once: the permissions come
	// from the database, not from the token.
	subs, err := support.API.ListSubscriptions(ctx, "", licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_UNSPECIFIED)
	require.NoError(t, err)
	assert.NotNil(t, subs)

	_, err = support.API.ListAllPlans(ctx)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")

	if len(subs) > 0 {
		_, err = support.API.ExtendSubscription(ctx, subs[0].GetSubscription().GetId(), 1)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")
	}

	_, err = support.API.ListPlatformRoles(ctx)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")

	staff, err := admin.API.ListPlatformStaff(ctx)
	require.NoError(t, err)

	var found bool

	for _, member := range staff {
		if member.GetEmail() == support.Email {
			found = true

			assert.False(t, member.GetPlatformAdmin())
			require.Len(t, member.GetGroups(), 1)
			assert.Equal(t, tm.group.GetId(), member.GetGroups()[0].GetId())
		}
	}

	assert.True(t, found, "the new member is listed")

	require.NoError(t, admin.API.RemovePlatformStaffMember(ctx, tm.group.GetId(), support.Session.GetUser().GetId()))

	_, err = support.API.ListSubscriptions(ctx, "", 0)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")

	after, err := support.API.GetMyPlatformAccess(ctx)
	require.NoError(t, err)
	assert.Empty(t, after.GetPermissions())
}

// Scenario: E2E-PLT-002: a change of a role applies to its members on their
// next request, a role in use cannot be deleted, and deleting a group takes
// its members' access with it.
func TestRoleChangesApplyAtOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	user := fixture.NewVerifiedOwner(t, "plt002")

	tm := newTeam(t, "desk", planRead)
	tm.join(t, admin, user)

	_, err := user.API.ListAllPlans(ctx)
	require.NoError(t, err)

	_, err = user.API.UpdatePlan(ctx, "any", &licensev1.Plan{Name: "x"}, "name")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")

	updated, err := admin.API.UpdatePlatformRole(ctx, tm.role.GetId(), &platformv1.PlatformRole{
		Name: tm.role.GetName(), Permissions: []platformv1.PlatformPermission{subscriptionRead},
	})
	require.NoError(t, err)
	assert.Equal(t, []platformv1.PlatformPermission{subscriptionRead}, updated.GetPermissions())

	_, err = user.API.ListAllPlans(ctx)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")

	_, err = user.API.ListSubscriptions(ctx, "", 0)
	require.NoError(t, err)

	// A write permission includes the read permission of its module.
	written, err := admin.API.UpdatePlatformRole(ctx, tm.role.GetId(), &platformv1.PlatformRole{
		Name: tm.role.GetName(), Permissions: []platformv1.PlatformPermission{planWrite},
	})
	require.NoError(t, err)
	assert.Equal(t, []platformv1.PlatformPermission{planRead, planWrite}, written.GetPermissions())

	err = admin.API.DeletePlatformRole(ctx, tm.role.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "PLATFORM_ROLE_IN_USE")

	group, err := admin.API.GetPlatformGroup(ctx, tm.group.GetId())
	require.NoError(t, err)
	assert.EqualValues(t, 1, group.GetGroup().GetMemberCount())
	require.Len(t, group.GetMembers(), 1)

	require.NoError(t, admin.API.DeletePlatformGroup(ctx, tm.group.GetId()))

	_, err = user.API.ListAllPlans(ctx)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")

	require.NoError(t, admin.API.DeletePlatformRole(ctx, tm.role.GetId()))
}

// Scenario: E2E-PLT-003: admin staff can only give permissions they hold
// themselves, cannot invite themselves, and cannot reach an admin's rights.
func TestStaffCannotRaiseTheirOwnAccess(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	manager := fixture.NewVerifiedOwner(t, "plt003")
	friend := fixture.NewVerifiedOwner(t, "plt003f")

	tm := newTeam(t, "managers", staffWrite, subscriptionRead)
	tm.join(t, admin, manager)

	_, err := manager.API.CreatePlatformRole(ctx, &platformv1.PlatformRole{Name: uniqueName("sneaky"), Permissions: []platformv1.PlatformPermission{planWrite}})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_NOT_HELD")

	_, err = manager.API.CreatePlatformInvitation(ctx, manager.Email, tm.group.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "CANNOT_INVITE_SELF")

	_, err = manager.API.CreatePlatformInvitation(ctx, admin.Email, tm.group.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	// A role the manager can hand out works; a role beyond them does not.
	reader, err := manager.API.CreatePlatformRole(ctx, &platformv1.PlatformRole{
		Name: uniqueName("reader"), Permissions: []platformv1.PlatformPermission{subscriptionRead},
	})
	require.NoError(t, err)

	t.Cleanup(func() { _ = admin.API.DeletePlatformRole(t.Context(), reader.GetId()) })

	power, err := admin.API.CreatePlatformRole(ctx, &platformv1.PlatformRole{Name: uniqueName("power"), Permissions: []platformv1.PlatformPermission{planWrite, auditRead}})
	require.NoError(t, err)

	t.Cleanup(func() { _ = admin.API.DeletePlatformRole(t.Context(), power.GetId()) })

	_, err = manager.API.UpdatePlatformRole(ctx, power.GetId(), &platformv1.PlatformRole{Name: "renamed", Permissions: power.GetPermissions()})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_NOT_HELD")

	_, err = manager.API.CreatePlatformGroup(ctx, &platformv1.PlatformStaffGroup{Name: uniqueName("mine"), RoleId: power.GetId()})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_NOT_HELD")

	// The manager may invite a friend into their own group.
	inv, err := manager.API.CreatePlatformInvitation(ctx, friend.Email, tm.group.GetId())
	require.NoError(t, err)

	_, err = friend.API.AcceptPlatformInvitation(ctx, inv.GetId())
	require.NoError(t, err)

	access, err := friend.API.GetMyPlatformAccess(ctx)
	require.NoError(t, err)
	assert.True(t, hasPermission(access, staffWrite))
	assert.False(t, hasPermission(access, subscriptionWrite))
}

// Scenario: E2E-PLT-004: invitation rules. An unknown e-mail is refused; an
// invitation can be revoked or declined; nobody can answer another user's
// invitation; a second pending invitation is refused.
func TestInvitationRules(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	invitee := fixture.NewVerifiedOwner(t, "plt004")
	stranger := fixture.NewVerifiedOwner(t, "plt004s")

	tm := newTeam(t, "inviting", subscriptionRead)

	_, err := admin.API.CreatePlatformInvitation(ctx, "nobody."+invitee.Email, tm.group.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "USER_NOT_FOUND")

	first, err := admin.API.CreatePlatformInvitation(ctx, invitee.Email, tm.group.GetId())
	require.NoError(t, err)

	_, err = admin.API.CreatePlatformInvitation(ctx, invitee.Email, tm.group.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "ALREADY_INVITED")

	_, err = stranger.API.AcceptPlatformInvitation(ctx, first.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	revoked, err := admin.API.RevokePlatformInvitation(ctx, first.GetId())
	require.NoError(t, err)
	assert.Equal(t, platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_REVOKED, revoked.GetStatus())

	_, err = invitee.API.AcceptPlatformInvitation(ctx, first.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "INVITATION_NOT_PENDING")

	second, err := admin.API.CreatePlatformInvitation(ctx, invitee.Email, tm.group.GetId())
	require.NoError(t, err)

	declined, err := invitee.API.DeclinePlatformInvitation(ctx, second.GetId())
	require.NoError(t, err)
	assert.Equal(t, platformv1.PlatformInvitationStatus_PLATFORM_INVITATION_STATUS_DECLINED, declined.GetStatus())

	access, err := invitee.API.GetMyPlatformAccess(ctx)
	require.NoError(t, err)
	assert.Empty(t, access.GetPermissions(), "declining gives no access")

	all, err := admin.API.ListPlatformInvitations(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)
}

// Scenario: E2E-PLT-005: an owner or any other user has no platform access,
// and every platform endpoint answers them 403; an anonymous caller 401.
func TestOrdinaryUsersHaveNoPlatformAccess(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "plt005")

	access, err := owner.API.GetMyPlatformAccess(ctx)
	require.NoError(t, err)
	assert.False(t, access.GetPlatformAdmin())
	assert.Empty(t, access.GetPermissions())

	for name, call := range map[string]func() error{
		"permissions": func() error { _, err := owner.API.ListPlatformPermissions(ctx); return err },
		"roles":       func() error { _, err := owner.API.ListPlatformRoles(ctx); return err },
		"groups":      func() error { _, err := owner.API.ListPlatformGroups(ctx); return err },
		"staff":       func() error { _, err := owner.API.ListPlatformStaff(ctx); return err },
		"invitations": func() error { _, err := owner.API.ListPlatformInvitations(ctx); return err },
		"create role": func() error {
			_, err := owner.API.CreatePlatformRole(ctx, &platformv1.PlatformRole{Name: uniqueName("owner")})
			return err
		},
	} {
		t.Log("checking", name)
		fixture.RequireAPIError(t, call(), http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")
	}

	_, err = fixture.Anonymous(t).GetMyPlatformAccess(ctx)
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "")

	admin := fixture.PlatformAdmin(t)

	adminAccess, err := admin.API.GetMyPlatformAccess(ctx)
	require.NoError(t, err)
	assert.True(t, adminAccess.GetPlatformAdmin())
	assert.Len(t, adminAccess.GetPermissions(), 12, "a Platform Admin holds every permission")

	catalogue, err := admin.API.ListPlatformPermissions(ctx)
	require.NoError(t, err)
	assert.Len(t, catalogue, 12)
}
