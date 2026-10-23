//go:build integration

package repository_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/repository"
)

func seedUser(t *testing.T, admin *sql.DB, email, status string) string {
	t.Helper()

	id := uuid.NewString()
	_, err := admin.ExecContext(t.Context(), `INSERT INTO users (id, email, full_name, status) VALUES ($1, $2, $2, $3)`, id, email, status)
	require.NoError(t, err)

	return id
}

// TestPlatformAccessIsReadFromTheDatabase covers the rule behind the
// Definition of Done of Release 8.1: the permissions of a user are the union
// of the roles of their groups, a Platform Admin holds everything, and a
// removal applies on the next read.
func TestPlatformAccessIsReadFromTheDatabase(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	repo := repository.NewPostgres(pg.AppPool(t))
	ctx := t.Context()

	adminUser := seedUser(t, pg.Admin, "admin@x.test", "active")
	billing := seedUser(t, pg.Admin, "bill@x.test", "active")
	suspended := seedUser(t, pg.Admin, "off@x.test", "suspended")
	nobody := seedUser(t, pg.Admin, "none@x.test", "active")

	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO platform_admins (user_id) VALUES ($1), ($2)`, adminUser, suspended)
	require.NoError(t, err)

	role := &domain.Role{Name: "Billing", Permissions: []access.PlatformPermission{access.PlatformSubscriptionRead}}
	require.NoError(t, repo.CreateRole(ctx, role))

	second := &domain.Role{Name: "Plans", Permissions: []access.PlatformPermission{access.PlatformPlanRead, access.PlatformPlanWrite}}
	require.NoError(t, repo.CreateRole(ctx, second))

	group := &domain.Group{Name: "Billing support", RoleID: role.ID}
	require.NoError(t, repo.CreateGroup(ctx, group))

	other := &domain.Group{Name: "Plan desk", RoleID: second.ID}
	require.NoError(t, repo.CreateGroup(ctx, other))

	require.NoError(t, repo.AddMember(ctx, group.ID, billing))
	require.NoError(t, repo.AddMember(ctx, group.ID, billing), "adding twice is harmless")

	acc, err := repo.PlatformAccess(ctx, billing)
	require.NoError(t, err)
	assert.False(t, acc.Admin)
	assert.Equal(t, []access.PlatformPermission{access.PlatformSubscriptionRead}, acc.Permissions)

	require.NoError(t, repo.AddMember(ctx, other.ID, billing))

	acc, err = repo.PlatformAccess(ctx, billing)
	require.NoError(t, err)
	assert.Equal(t, []access.PlatformPermission{access.PlatformPlanRead, access.PlatformPlanWrite, access.PlatformSubscriptionRead}, acc.Permissions,
		"several groups give the union of their roles")

	// A role edit applies at once.
	role.Permissions = []access.PlatformPermission{access.PlatformSubscriptionWrite}
	require.NoError(t, repo.UpdateRole(ctx, role))

	acc, err = repo.PlatformAccess(ctx, billing)
	require.NoError(t, err)
	assert.Contains(t, acc.Permissions, access.PlatformSubscriptionWrite)

	require.NoError(t, repo.RemoveMember(ctx, group.ID, billing))
	require.NoError(t, repo.RemoveMember(ctx, other.ID, billing))

	acc, err = repo.PlatformAccess(ctx, billing)
	require.NoError(t, err)
	assert.False(t, acc.Any(), "removing the user from the groups removes the access")

	admin, err := repo.PlatformAccess(ctx, adminUser)
	require.NoError(t, err)
	assert.True(t, admin.Admin)

	gone, err := repo.PlatformAccess(ctx, suspended)
	require.NoError(t, err)
	assert.False(t, gone.Any(), "a user who is not active has no platform access, even a listed admin")

	none, err := repo.PlatformAccess(ctx, nobody)
	require.NoError(t, err)
	assert.False(t, none.Any())

	unknown, err := repo.PlatformAccess(ctx, uuid.NewString())
	require.NoError(t, err)
	assert.False(t, unknown.Any())

	has, err := repo.HasPlatformAccess(ctx, adminUser)
	require.NoError(t, err)
	assert.True(t, has)
}

func TestRolesGroupsAndInvitationsRoundTrip(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	repo := repository.NewPostgres(pg.AppPool(t))
	ctx := t.Context()

	inviter := seedUser(t, pg.Admin, "admin@x.test", "active")
	invitee := seedUser(t, pg.Admin, "bill@x.test", "active")

	role := &domain.Role{Name: "Billing", Description: "d", Permissions: []access.PlatformPermission{access.PlatformSubscriptionRead}}
	require.NoError(t, repo.CreateRole(ctx, role))
	require.ErrorIs(t, repo.CreateRole(ctx, &domain.Role{Name: "BILLING"}), domain.ErrRoleNameTaken, "names are unique without regard to case")

	got, err := repo.FindRole(ctx, role.ID)
	require.NoError(t, err)
	assert.Equal(t, role.Permissions, got.Permissions)

	_, err = repo.FindRole(ctx, uuid.NewString())
	require.ErrorIs(t, err, domain.ErrRoleNotFound)

	other := &domain.Role{Name: "Other"}
	require.NoError(t, repo.CreateRole(ctx, other))

	other.Name = "billing"
	require.ErrorIs(t, repo.UpdateRole(ctx, other), domain.ErrRoleNameTaken)
	require.ErrorIs(t, repo.UpdateRole(ctx, &domain.Role{ID: uuid.NewString(), Name: "x"}), domain.ErrRoleNotFound)

	roles, err := repo.ListRoles(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 2)
	assert.Equal(t, "Billing", roles[0].Name)

	group := roundTripGroups(t, repo, role, other)
	roundTripInvitations(t, pg, repo, group, inviter, invitee)
}

func roundTripGroups(t *testing.T, repo *repository.Postgres, role, other *domain.Role) *domain.Group {
	t.Helper()

	ctx := t.Context()

	group := &domain.Group{Name: "Desk", Description: "d", RoleID: role.ID}
	require.NoError(t, repo.CreateGroup(ctx, group))
	require.ErrorIs(t, repo.CreateGroup(ctx, &domain.Group{Name: "desk", RoleID: role.ID}), domain.ErrGroupNameTaken)

	group.RoleID = other.ID
	require.NoError(t, repo.UpdateGroup(ctx, group))
	require.ErrorIs(t, repo.UpdateGroup(ctx, &domain.Group{ID: uuid.NewString(), Name: "x", RoleID: role.ID}), domain.ErrGroupNotFound)

	foundGroup, err := repo.FindGroup(ctx, group.ID)
	require.NoError(t, err)
	assert.Equal(t, "Other", foundGroup.RoleName)

	_, err = repo.FindGroup(ctx, uuid.NewString())
	require.ErrorIs(t, err, domain.ErrGroupNotFound)

	require.ErrorIs(t, repo.DeleteRole(ctx, other.ID), domain.ErrRoleInUse)
	require.ErrorIs(t, repo.DeleteRole(ctx, uuid.NewString()), domain.ErrRoleNotFound)

	return group
}

func roundTripInvitations(t *testing.T, pg *pgtest.Postgres, repo *repository.Postgres, group *domain.Group, inviter, invitee string) {
	t.Helper()

	ctx := t.Context()

	member, err := repo.IsMember(ctx, group.ID, invitee)
	require.NoError(t, err)
	assert.False(t, member)
	require.ErrorIs(t, repo.RemoveMember(ctx, group.ID, invitee), domain.ErrMemberNotFound)

	inv := &domain.Invitation{
		GroupID: group.ID, UserID: invitee, Status: domain.InvitationPending, InvitedBy: inviter, ExpiresAt: time.Now().Add(time.Hour),
	}
	require.NoError(t, repo.CreateInvitation(ctx, inv))
	require.ErrorIs(t, repo.CreateInvitation(ctx, &domain.Invitation{
		GroupID: group.ID, UserID: invitee, Status: domain.InvitationPending, InvitedBy: inviter, ExpiresAt: time.Now().Add(time.Hour),
	}), domain.ErrAlreadyInvited, "one pending invitation per user and group")

	loaded, err := repo.FindInvitation(ctx, inv.ID)
	require.NoError(t, err)
	assert.Equal(t, "Desk", loaded.GroupName)
	assert.Equal(t, "Other", loaded.RoleName)
	assert.Equal(t, domain.InvitationPending, loaded.Status)

	now := time.Now().UTC()
	require.NoError(t, loaded.Accept(now))
	require.NoError(t, repo.UpdateInvitation(ctx, loaded))
	require.ErrorIs(t, repo.UpdateInvitation(ctx, &domain.Invitation{ID: uuid.NewString(), Status: domain.InvitationRevoked}), domain.ErrInvitationNotFound)

	_, err = repo.FindInvitation(ctx, uuid.NewString())
	require.ErrorIs(t, err, domain.ErrInvitationNotFound)

	mine, err := repo.ListInvitationsByUser(ctx, invitee)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, domain.InvitationAccepted, mine[0].Status)
	assert.NotNil(t, mine[0].RespondedAt)

	all, err := repo.ListInvitations(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	// A stale pending invitation is expired, which lets a new one in.
	stale := &domain.Invitation{
		GroupID: group.ID, UserID: inviter, Status: domain.InvitationPending, InvitedBy: inviter, ExpiresAt: time.Now().Add(-time.Hour),
	}
	require.NoError(t, repo.CreateInvitation(ctx, stale))
	require.NoError(t, repo.ExpireStale(ctx, group.ID, inviter, time.Now()))

	reloaded, err := repo.FindInvitation(ctx, stale.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.InvitationExpired, reloaded.Status)

	require.NoError(t, repo.AddMember(ctx, group.ID, invitee))

	members, err := repo.GroupMembers(ctx, group.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{invitee}, members)

	memberships, err := repo.Memberships(ctx)
	require.NoError(t, err)
	assert.Equal(t, []domain.GroupRef{{ID: group.ID, Name: "Desk"}}, memberships[invitee])

	groups, err := repo.ListGroups(ctx)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, 1, groups[0].MemberCount)

	// Deleting the group removes its members and invitations.
	require.NoError(t, repo.DeleteGroup(ctx, group.ID))
	require.ErrorIs(t, repo.DeleteGroup(ctx, group.ID), domain.ErrGroupNotFound)

	_, err = repo.FindInvitation(ctx, inv.ID)
	require.ErrorIs(t, err, domain.ErrInvitationNotFound)

	rest, err := repo.Memberships(ctx)
	require.NoError(t, err)
	assert.Empty(t, rest)

	adminID, err := repo.IsAdmin(ctx, inviter)
	require.NoError(t, err)
	assert.False(t, adminID)

	_, err = pg.Admin.ExecContext(context.Background(), `INSERT INTO platform_admins (user_id) VALUES ($1)`, inviter)
	require.NoError(t, err)

	ids, err := repo.AdminIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{inviter}, ids)

	adminID, err = repo.IsAdmin(ctx, inviter)
	require.NoError(t, err)
	assert.True(t, adminID)
}
