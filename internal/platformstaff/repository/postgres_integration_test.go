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
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
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

// TestAMalformedIDIsNotFound: an id that is not a UUID in a request is "not
// found", as a missing one is, and not a database error (backlog G-17).
func TestAMalformedIDIsNotFound(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	repo := repository.NewPostgres(pg.AppPool(t))
	ctx := t.Context()

	_, err := repo.FindRole(ctx, "not-a-uuid")
	require.ErrorIs(t, err, domain.ErrRoleNotFound)

	// uuid.Validate accepts this form, Postgres does not.
	_, err = repo.FindRole(ctx, "urn:uuid:"+uuid.NewString())
	require.ErrorIs(t, err, domain.ErrRoleNotFound)

	_, err = repo.FindGroup(ctx, "not-a-uuid")
	require.ErrorIs(t, err, domain.ErrGroupNotFound)

	_, err = repo.FindInvitation(ctx, "not-a-uuid")
	require.ErrorIs(t, err, domain.ErrInvitationNotFound)

	require.ErrorIs(t, repo.RemoveMember(ctx, uuid.NewString(), "not-a-uuid"), domain.ErrMemberNotFound)
	require.ErrorIs(t, repo.RemoveMember(ctx, "not-a-uuid", uuid.NewString()), domain.ErrMemberNotFound)
}

// TestPlatformAuditLogShowsOnlyEntriesWithoutAGarage covers the read behind
// audit.read: the entries that belong to no garage, newest first, filtered
// and paged, while the garages' own entries stay out and the setting that
// opens the log is closed again before the transaction goes on.
func TestPlatformAuditLogShowsOnlyEntriesWithoutAGarage(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	pool := pg.AppPool(t)
	repo := repository.NewPostgres(pool)
	manager := dbpkg.NewTxManager(pool)
	ctx := t.Context()

	actor := seedUser(t, pg.Admin, "actor@x.test", "active")
	garage := uuid.NewString()
	base := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)

	for i, e := range []struct{ garage, actor, action, entityType string }{
		{"", actor, "plan.created", "plan"},
		{"", actor, "plan.updated", "plan"},
		{"", "", "platform_staff.group_created", "platform_group"},
		{"", actor, "platform.signed_in", "user"},
		{garage, actor, "role.updated", "role"},
	} {
		_, err := pg.Admin.ExecContext(ctx, `
			INSERT INTO audit_log (actor_user_id, garage_id, action, entity_type, entity_id, after, ip, created_at)
			VALUES (NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, $4, 'x', '{"n":1}', '1.2.3.4', $5)`,
			e.actor, e.garage, e.action, e.entityType, base.Add(time.Duration(i)*time.Hour))
		require.NoError(t, err)
	}

	list := func(filter domain.AuditFilter, page domain.Page) (entries []*domain.AuditEntry, total int) {
		t.Helper()

		require.NoError(t, manager.WithTransaction(ctx, dbpkg.UserScope(uuid.NewString()), func(ctx context.Context) error {
			var err error

			entries, total, err = repo.ListAuditLog(ctx, filter, page)

			return err
		}))

		return entries, total
	}

	all, total := list(domain.AuditFilter{}, domain.Page{Page: 1, PageSize: 10})
	require.Equal(t, 4, total, "the garage's entry is not part of the platform log")
	require.Len(t, all, 4)
	assert.Equal(t, "platform.signed_in", all[0].Action, "newest first")
	assert.Equal(t, "plan.created", all[3].Action)
	assert.Equal(t, actor, all[0].ActorUserID)
	assert.Empty(t, all[1].ActorUserID, "a system entry has no actor")
	assert.JSONEq(t, `{"n":1}`, all[0].AfterJSON)
	assert.Empty(t, all[0].BeforeJSON)
	assert.Equal(t, "1.2.3.4", all[0].IP)

	plans, total := list(domain.AuditFilter{Action: "plan."}, domain.Page{Page: 1, PageSize: 10})
	assert.Equal(t, 2, total)
	assert.Len(t, plans, 2)

	_, total = list(domain.AuditFilter{Action: "plan_"}, domain.Page{Page: 1, PageSize: 10})
	assert.Zero(t, total, "an underscore in the filter is a character, not a wildcard")

	byType, _ := list(domain.AuditFilter{EntityType: "user"}, domain.Page{Page: 1, PageSize: 10})
	assert.Len(t, byType, 1)

	byActor, total := list(domain.AuditFilter{ActorUserID: actor}, domain.Page{Page: 1, PageSize: 10})
	assert.Equal(t, 3, total)
	assert.Len(t, byActor, 3)

	from, to := base.Add(time.Hour), base.Add(3*time.Hour)
	window, total := list(domain.AuditFilter{From: &from, To: &to}, domain.Page{Page: 1, PageSize: 10})
	assert.Equal(t, 2, total, "from is inclusive, to is exclusive")
	assert.Equal(t, "plan.updated", window[1].Action)

	second, total := list(domain.AuditFilter{}, domain.Page{Page: 2, PageSize: 3})
	assert.Equal(t, 4, total)
	require.Len(t, second, 1)
	assert.Equal(t, "plan.created", second[0].Action)

	require.NoError(t, manager.WithTransaction(ctx, dbpkg.UserScope(uuid.NewString()), func(ctx context.Context) error {
		_, _, err := repo.ListAuditLog(ctx, domain.AuditFilter{}, domain.Page{Page: 1, PageSize: 10})
		require.NoError(t, err)

		var n int
		require.NoError(t, dbpkg.QuerierFromContext(ctx, pool).QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n))
		assert.Zero(t, n, "the log is closed again once the read is done")

		return nil
	}))

	_, _, err := repo.ListAuditLog(ctx, domain.AuditFilter{}, domain.Page{Page: 1, PageSize: 10})
	require.Error(t, err, "outside a transaction the setting would not be local to the request")
}
