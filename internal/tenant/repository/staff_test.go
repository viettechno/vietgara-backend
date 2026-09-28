package repository_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

func TestStaffRepository(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresStaffRepository(nil)
	member := &domain.Staff{GarageID: "g-1", AccountID: "acc-1", Status: domain.StaffStatusActive}

	tx := dbtest.New(t).
		ExpectQueryRow("s.id = $2", dbtest.Row(staffRow()...)).
		ExpectQueryRow("s.account_id = $2", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQuery("WHERE s.garage_id = $1", dbtest.NewRows(staffRow()), nil).
		ExpectQuery("WHERE s.garage_id = $1", nil, errBoom).
		ExpectQuery("WHERE s.garage_id = $1", dbtest.NewRows([]any{"x"}), nil).
		ExpectQueryRow("INSERT INTO garage_staff", dbtest.Row("s-2", now, now)).
		ExpectQueryRow("INSERT INTO garage_staff", dbtest.ErrRow(dbtest.UniqueViolation("idx_garage_staff_garage_account_unique"))).
		ExpectQueryRow("UPDATE garage_staff", dbtest.Row(now)).
		ExpectQueryRow("UPDATE garage_staff", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectExec("DELETE FROM staff_group_members", dbtest.Tag(1), nil).
		ExpectExec("INSERT INTO staff_group_members", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM staff_group_members", dbtest.Tag(0), nil).
		ExpectExec("DELETE FROM staff_group_members", dbtest.Tag(0), errBoom).
		ExpectExec("DELETE FROM garage_staff", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM garage_staff", dbtest.Tag(0), nil).
		ExpectExec("DELETE FROM garage_staff", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	s, err := repo.FindByID(ctx, "g-1", "s-1")
	require.NoError(t, err)
	assert.Equal(t, []access.Permission{access.CustomerRead, access.ReportRead}, s.Permissions)
	assert.Equal(t, []string{"grp-1"}, s.GroupIDs)

	_, err = repo.FindByAccount(ctx, "g-1", "acc-1")
	require.ErrorIs(t, err, domain.ErrStaffNotFound)

	list, err := repo.List(ctx, "g-1")
	require.NoError(t, err)
	assert.Len(t, list, 1)

	_, err = repo.List(ctx, "g-1")
	require.ErrorIs(t, err, errBoom)
	_, err = repo.List(ctx, "g-1")
	require.Error(t, err)

	require.NoError(t, repo.Create(ctx, member))
	assert.Equal(t, "s-2", member.ID)
	require.ErrorIs(t, repo.Create(ctx, member), domain.ErrAlreadyMember)

	require.NoError(t, repo.UpdateStatus(ctx, member))
	require.ErrorIs(t, repo.UpdateStatus(ctx, member), domain.ErrStaffNotFound)

	require.NoError(t, repo.SetGroups(ctx, "g-1", "s-2", []string{"grp-1"}))
	require.NoError(t, repo.SetGroups(ctx, "g-1", "s-2", nil))
	require.ErrorIs(t, repo.SetGroups(ctx, "g-1", "s-2", nil), errBoom)

	require.NoError(t, repo.Delete(ctx, "g-1", "s-2"))
	require.ErrorIs(t, repo.Delete(ctx, "g-1", "s-2"), domain.ErrStaffNotFound)
	require.ErrorIs(t, repo.Delete(ctx, "g-1", "s-2"), errBoom)
}

func TestRoleRepository(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRoleRepository(nil)
	key := domain.DefaultAccountant
	role := &domain.Role{OwnerAccountID: "owner", Name: "Kế toán", Permissions: []access.Permission{access.BillingRead}, DefaultKey: &key}
	empty := &domain.Role{OwnerAccountID: "owner", Name: "Empty"}

	tx := dbtest.New(t).
		ExpectQuery("r.owner_account_id = $1", dbtest.NewRows(roleRow()), nil).
		ExpectQuery("r.owner_account_id = $1", nil, errBoom).
		ExpectQuery("r.owner_account_id = $1", dbtest.NewRows([]any{1}), nil).
		ExpectQueryRow("r.id = $1", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("INSERT INTO roles", dbtest.Row("r-2", now, now)).
		ExpectExec("INSERT INTO role_permissions", dbtest.Tag(1), nil).
		ExpectQueryRow("INSERT INTO roles", dbtest.ErrRow(dbtest.UniqueViolation("idx_roles_owner_name_unique"))).
		ExpectQueryRow("INSERT INTO roles", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("ON CONFLICT DO NOTHING", dbtest.Row("r-3", now, now)).
		ExpectExec("INSERT INTO role_permissions", dbtest.Tag(1), nil).
		ExpectQueryRow("ON CONFLICT DO NOTHING", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("ON CONFLICT DO NOTHING", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("UPDATE roles", dbtest.Row(now)).
		ExpectExec("DELETE FROM role_permissions", dbtest.Tag(1), nil).
		ExpectQueryRow("UPDATE roles", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("UPDATE roles", dbtest.ErrRow(dbtest.UniqueViolation("x"))).
		ExpectQueryRow("UPDATE roles", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("UPDATE roles", dbtest.Row(now)).
		ExpectExec("DELETE FROM role_permissions", dbtest.Tag(0), errBoom).
		ExpectExec("DELETE FROM roles", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM roles", dbtest.Tag(0), dbtest.ForeignKeyViolation()).
		ExpectExec("DELETE FROM roles", dbtest.Tag(0), nil).
		ExpectExec("DELETE FROM roles", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	roles, err := repo.ListByOwner(ctx, "owner")
	require.NoError(t, err)
	require.Len(t, roles, 1)
	assert.Equal(t, domain.DefaultAccountant, *roles[0].DefaultKey)
	assert.Equal(t, 2, roles[0].GroupCount)

	_, err = repo.ListByOwner(ctx, "owner")
	require.ErrorIs(t, err, errBoom)
	_, err = repo.ListByOwner(ctx, "owner")
	require.Error(t, err)

	_, err = repo.FindByID(ctx, "r-1")
	require.ErrorIs(t, err, domain.ErrRoleNotFound)

	require.NoError(t, repo.Create(ctx, role))
	require.ErrorIs(t, repo.Create(ctx, role), domain.ErrRoleNameTaken)
	require.ErrorIs(t, repo.Create(ctx, role), errBoom)

	inserted, err := repo.CreateDefault(ctx, role)
	require.NoError(t, err)
	assert.True(t, inserted)
	inserted, err = repo.CreateDefault(ctx, role)
	require.NoError(t, err)
	assert.False(t, inserted)

	_, err = repo.CreateDefault(ctx, role)
	require.ErrorIs(t, err, errBoom)

	require.NoError(t, repo.Update(ctx, empty))
	require.ErrorIs(t, repo.Update(ctx, empty), domain.ErrRoleNotFound)
	require.ErrorIs(t, repo.Update(ctx, empty), domain.ErrRoleNameTaken)
	require.ErrorIs(t, repo.Update(ctx, empty), errBoom)
	require.ErrorIs(t, repo.Update(ctx, empty), errBoom)

	require.NoError(t, repo.Delete(ctx, "r-1"))
	require.ErrorIs(t, repo.Delete(ctx, "r-1"), domain.ErrRoleInUse)
	require.ErrorIs(t, repo.Delete(ctx, "r-1"), domain.ErrRoleNotFound)
	require.ErrorIs(t, repo.Delete(ctx, "r-1"), errBoom)
}

func TestGroupRepository(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresGroupRepository(nil)
	key := domain.DefaultTechnician
	group := &domain.StaffGroup{GarageID: "g-1", Name: "Kỹ thuật", RoleID: "r-1", DefaultKey: &key}
	withKey := groupRow()
	withKey[6] = "technician"

	tx := dbtest.New(t).
		ExpectQuery("WHERE sg.garage_id = $1", dbtest.NewRows(groupRow(), withKey), nil).
		ExpectQuery("WHERE sg.garage_id = $1", nil, errBoom).
		ExpectQuery("WHERE sg.garage_id = $1", dbtest.NewRows([]any{1}), nil).
		ExpectQueryRow("sg.id = $2", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("INSERT INTO staff_groups", dbtest.Row("grp-2", now, now)).
		ExpectQueryRow("INSERT INTO staff_groups", dbtest.ErrRow(dbtest.UniqueViolation("x"))).
		ExpectQueryRow("ON CONFLICT DO NOTHING", dbtest.Row("grp-3", now, now)).
		ExpectQueryRow("ON CONFLICT DO NOTHING", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("UPDATE staff_groups", dbtest.Row(now)).
		ExpectQueryRow("UPDATE staff_groups", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("UPDATE staff_groups", dbtest.ErrRow(dbtest.UniqueViolation("x"))).
		ExpectExec("DELETE FROM staff_groups", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM staff_groups", dbtest.Tag(0), nil).
		ExpectExec("DELETE FROM staff_groups", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	groups, err := repo.List(ctx, "g-1")
	require.NoError(t, err)
	require.Len(t, groups, 2)
	assert.Nil(t, groups[0].DefaultKey)
	assert.Equal(t, domain.DefaultTechnician, *groups[1].DefaultKey)
	assert.Equal(t, 3, groups[0].MemberCount)

	_, err = repo.List(ctx, "g-1")
	require.ErrorIs(t, err, errBoom)
	_, err = repo.List(ctx, "g-1")
	require.Error(t, err)

	_, err = repo.FindByID(ctx, "g-1", "x")
	require.ErrorIs(t, err, domain.ErrGroupNotFound)

	require.NoError(t, repo.Create(ctx, group))
	require.ErrorIs(t, repo.Create(ctx, group), domain.ErrGroupNameTaken)

	inserted, err := repo.CreateDefault(ctx, group)
	require.NoError(t, err)
	assert.True(t, inserted)
	inserted, err = repo.CreateDefault(ctx, group)
	require.NoError(t, err)
	assert.False(t, inserted)

	require.NoError(t, repo.Update(ctx, group))
	require.ErrorIs(t, repo.Update(ctx, group), domain.ErrGroupNotFound)
	require.ErrorIs(t, repo.Update(ctx, group), domain.ErrGroupNameTaken)

	require.NoError(t, repo.Delete(ctx, "g-1", "grp-2"))
	require.ErrorIs(t, repo.Delete(ctx, "g-1", "grp-2"), domain.ErrGroupNotFound)
	require.ErrorIs(t, repo.Delete(ctx, "g-1", "grp-2"), errBoom)
}

func TestInvitationRepository(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresInvitationRepository(nil)
	inv := &domain.Invitation{GarageID: "g-1", AccountID: "acc-1", Status: domain.InvitationPending, InvitedBy: "owner", ExpiresAt: now}

	tx := dbtest.New(t).
		ExpectQueryRow("WHERE i.id = $1", dbtest.Row(invitationRow()...)).
		ExpectQueryRow("WHERE i.id = $1", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQuery("i.garage_id = $1", dbtest.NewRows(invitationRow()), nil).
		ExpectQuery("i.account_id = $1", nil, errBoom).
		ExpectQuery("i.account_id = $1", dbtest.NewRows([]any{1}), nil).
		ExpectQueryRow("INSERT INTO staff_invitations", dbtest.Row("i-2", now, now)).WithArgs(func(args []any) {
		assert.Equal(t, []string{}, args[2])
	}).
		ExpectQueryRow("INSERT INTO staff_invitations", dbtest.ErrRow(dbtest.UniqueViolation("idx_staff_invitations_pending_unique"))).
		ExpectQueryRow("UPDATE staff_invitations", dbtest.Row(now)).
		ExpectQueryRow("UPDATE staff_invitations", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectExec("SET status = 'expired'", dbtest.Tag(1), nil)
	ctx := tx.Context(t.Context())

	got, err := repo.FindByID(ctx, "i-1")
	require.NoError(t, err)
	assert.Equal(t, domain.InvitationPending, got.Status)
	assert.Equal(t, "Gara", got.GarageName)

	_, err = repo.FindByID(ctx, "x")
	require.ErrorIs(t, err, domain.ErrInvitationNotFound)

	list, err := repo.ListByGarage(ctx, "g-1")
	require.NoError(t, err)
	assert.Len(t, list, 1)

	_, err = repo.ListByAccount(ctx, "acc-1")
	require.ErrorIs(t, err, errBoom)
	_, err = repo.ListByAccount(ctx, "acc-1")
	require.Error(t, err)

	require.NoError(t, repo.Create(ctx, inv))
	require.ErrorIs(t, repo.Create(ctx, inv), domain.ErrAlreadyInvited)
	require.NoError(t, repo.Update(ctx, inv))
	require.ErrorIs(t, repo.Update(ctx, inv), domain.ErrInvitationNotFound)
	require.NoError(t, repo.ExpireStale(ctx, "g-1", "acc-1"))
}
