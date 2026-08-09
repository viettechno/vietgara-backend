package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

func TestPostgresGarageStaffRepository_Create(t *testing.T) {
	t.Run("inserts the staff member and returns back id and timestamps", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		s := &domain.GarageStaff{
			ID: "staff-1", GarageID: "garage-1", AccountID: "acc-2",
			Role: domain.RoleTechnician, Status: domain.StaffStatusActive,
		}
		createdAt := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("INSERT INTO garage_staff"), []any{"staff-1", "garage-1", "acc-2", "technician", "active"}).
			Return(valuesRow(t, []any{"staff-1", createdAt, createdAt}))

		require.NoError(t, repo.Create(t.Context(), s))
		assert.Equal(t, "staff-1", s.ID)
		assert.Equal(t, createdAt, s.CreatedAt)
		assert.Equal(t, createdAt, s.UpdatedAt)
	})

	t.Run("propagates scan errors", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		boom := errors.New("insert boom")
		conn.EXPECT().
			QueryRow(mock.Anything, sqlContaining("INSERT INTO garage_staff"), mock.Anything).
			Return(errRow(t, boom))

		err := repo.Create(t.Context(), &domain.GarageStaff{ID: "s", GarageID: "g", AccountID: "a"})
		require.ErrorIs(t, err, boom)
	})
}

func TestPostgresGarageStaffRepository_ListByGarageID(t *testing.T) {
	t.Run("returns active staff with roles and statuses mapped", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		rows := resultRows(t, newRowsState([][]any{
			staffScanValues("staff-1", "garage-1", "acc-1", "owner", "active"),
			staffScanValues("staff-2", "garage-1", "acc-2", "service_advisor", "active"),
		}))
		conn.EXPECT().
			Query(t.Context(), sqlContaining("FROM garage_staff WHERE garage_id = $1"), []any{"garage-1"}).
			Return(rows, nil)

		list, err := repo.ListByGarageID(t.Context(), "garage-1")
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, domain.RoleOwner, list[0].Role)
		assert.Equal(t, domain.RoleServiceAdvisor, list[1].Role)
		assert.Equal(t, domain.StaffStatusActive, list[0].Status)
	})

	t.Run("propagates query errors", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		boom := errors.New("query boom")
		conn.EXPECT().
			Query(mock.Anything, sqlContaining("FROM garage_staff WHERE garage_id = $1"), []any{"garage-1"}).
			Return(nil, boom)

		_, err := repo.ListByGarageID(t.Context(), "garage-1")
		require.ErrorIs(t, err, boom)
	})
}

func TestPostgresGarageStaffRepository_FindByGarageAndAccount(t *testing.T) {
	t.Run("returns the staff member", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("FROM garage_staff WHERE garage_id = $1 AND account_id = $2"), []any{"garage-1", "acc-2"}).
			Return(valuesRow(t, staffScanValues("staff-1", "garage-1", "acc-2", "technician", "active")))

		s, err := repo.FindByGarageAndAccount(t.Context(), "garage-1", "acc-2")
		require.NoError(t, err)
		assert.Equal(t, "garage-1", s.GarageID)
		assert.Equal(t, domain.RoleTechnician, s.Role)
		assert.Equal(t, domain.StaffStatusActive, s.Status)
	})

	t.Run("propagates errors untouched (no ErrNoRows mapping here)", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		conn.EXPECT().
			QueryRow(mock.Anything, sqlContaining("FROM garage_staff WHERE garage_id = $1 AND account_id = $2"), mock.Anything).
			Return(errRow(t, pgx.ErrNoRows))

		_, err := repo.FindByGarageAndAccount(t.Context(), "garage-1", "nobody")
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})
}

func TestPostgresGarageStaffRepository_ListRolesForAccount(t *testing.T) {
	t.Run("returns the role assignments for every active membership", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		rows := resultRows(t, newRowsState([][]any{
			{"garage-1", "owner"},
			{"garage-2", "accountant"},
		}))
		conn.EXPECT().
			Query(t.Context(), sqlContaining("FROM garage_staff WHERE account_id = $1 AND status = 'active'"), []any{"acc-2"}).
			Return(rows, nil)

		got, err := repo.ListRolesForAccount(t.Context(), "acc-2")
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, repository.RoleAssignment{GarageID: "garage-1", Role: "owner"}, got[0])
		assert.Equal(t, repository.RoleAssignment{GarageID: "garage-2", Role: "accountant"}, got[1])
	})

	t.Run("propagates query errors", func(t *testing.T) {
		conn, pool := newQuerier(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		boom := errors.New("query boom")
		conn.EXPECT().
			Query(mock.Anything, sqlContaining("FROM garage_staff WHERE account_id = $1"), []any{"acc-2"}).
			Return(nil, boom)

		_, err := repo.ListRolesForAccount(t.Context(), "acc-2")
		require.ErrorIs(t, err, boom)
	})
}
