package repository_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/mocks"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// ---------------------------------------------------------------------------
// Fakes for pgx.Row / pgx.Rows
// ---------------------------------------------------------------------------

// stubRow lets tests hand a repository a row whose Scan copies a fixed set
// of values into the destinations (in order), or returns a fixed error.
type stubRow struct {
	values []any
	err    error
}

func (r *stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}

	return assignScanValues(dest, r.values)
}

// stubRows emulates a *pgx.Rows result set: a slice of fixed rows, an
// optional Scan failure at a chosen row, and a flag recording Close().
type stubRows struct {
	rows    [][]any
	scanAt  int
	scanErr error
	idx     int
	closed  bool
}

func newStubRows(rows [][]any) *stubRows {
	return &stubRows{rows: rows, scanAt: -1}
}

func (r *stubRows) Next() bool { return r.idx < len(r.rows) }

func (r *stubRows) Scan(dest ...any) error {
	if r.scanErr != nil && r.idx == r.scanAt {
		return r.scanErr
	}

	if err := assignScanValues(dest, r.rows[r.idx]); err != nil {
		return err
	}

	r.idx++

	return nil
}

func (r *stubRows) Close()                                       { r.closed = true }
func (r *stubRows) Err() error                                   { return nil }
func (r *stubRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *stubRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *stubRows) Values() ([]any, error)                       { return nil, nil }
func (r *stubRows) RawValues() [][]byte                          { return nil }
func (r *stubRows) Conn() *pgx.Conn                              { return nil }

func assignScanValues(dest, values []any) error {
	for i := range dest {
		value := values[i]

		if value == nil {
			continue
		}

		dv := reflect.ValueOf(dest[i]).Elem()
		sv := reflect.ValueOf(value)

		if !sv.Type().AssignableTo(dv.Type()) {
			return errors.New("scan: value type mismatch")
		}

		dv.Set(sv)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Unit helpers
// ---------------------------------------------------------------------------

var testTimestamp = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func garageScanRow(id, accountID, name, status string) []any {
	return []any{
		id, accountID, name, nil, nil, nil, "Asia/Ho_Chi_Minh", "VND", status,
		testTimestamp, testTimestamp,
	}
}

func staffScanValues(id, garageID, accountID, role, status string) []any {
	return []any{id, garageID, accountID, role, status, testTimestamp, testTimestamp}
}

func sqlContaining(fragment string) any {
	return mock.MatchedBy(func(sql string) bool {
		return strings.Contains(sql, fragment)
	})
}

func newConnPool(t *testing.T) (*mocks.MockConnPool, *dbpkg.Pool) {
	t.Helper()

	conn := mocks.NewMockConnPool(t)
	pool := dbpkg.NewPool(t.Context(), conn)

	return conn, pool
}

// ---------------------------------------------------------------------------
// PostgresGarageRepository
// ---------------------------------------------------------------------------

func TestPostgresGarageRepository_FindByID(t *testing.T) {
	t.Run("returns the garage with the tenant status mapped", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		id := "garage-1"
		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("FROM garages WHERE id = $1"), []any{id}).
			Return(&stubRow{values: garageScanRow(id, "acc-1", "Gara A", "active")})

		g, err := repo.FindByID(t.Context(), id)
		require.NoError(t, err)
		assert.Equal(t, id, g.ID)
		assert.Equal(t, "acc-1", g.AccountID)
		assert.Equal(t, "Gara A", g.Name)
		assert.Equal(t, "Asia/Ho_Chi_Minh", g.Timezone)
		assert.Equal(t, "VND", g.Currency)
		assert.Equal(t, domain.GarageStatusActive, g.Status)
		require.Nil(t, g.TaxCode)
		assert.Equal(t, testTimestamp, g.CreatedAt)
	})

	t.Run("maps ErrNoRows to ErrGarageNotFound", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		conn.EXPECT().
			QueryRow(mock.Anything, sqlContaining("FROM garages WHERE id = $1"), []any{"garage-1"}).
			Return(&stubRow{err: pgx.ErrNoRows})

		_, err := repo.FindByID(t.Context(), "garage-1")
		require.ErrorIs(t, err, domain.ErrGarageNotFound)
	})

	t.Run("propagates scan errors", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		boom := errors.New("scan boom")
		conn.EXPECT().
			QueryRow(mock.Anything, sqlContaining("FROM garages WHERE id = $1"), []any{"garage-1"}).
			Return(&stubRow{err: boom})

		_, err := repo.FindByID(t.Context(), "garage-1")
		require.ErrorIs(t, err, boom)
	})
}

func TestPostgresGarageRepository_ListByAccountID(t *testing.T) {
	t.Run("returns all rows in order and closes the result set", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		rows := newStubRows([][]any{
			garageScanRow("garage-1", "acc-1", "Gara A", "active"),
			garageScanRow("garage-2", "acc-1", "Gara B", "inactive"),
		})
		conn.EXPECT().
			Query(t.Context(), sqlContaining("FROM garages WHERE account_id = $1"), []any{"acc-1"}).
			Return(rows, nil)

		list, err := repo.ListByAccountID(t.Context(), "acc-1")
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, "garage-1", list[0].ID)
		assert.Equal(t, "garage-2", list[1].ID)
		assert.Equal(t, domain.GarageStatusInactive, list[1].Status)
		require.True(t, rows.closed)
	})

	t.Run("returns an empty slice when there are no rows", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		rows := newStubRows(nil)
		conn.EXPECT().
			Query(t.Context(), sqlContaining("FROM garages WHERE account_id = $1"), []any{"acc-1"}).
			Return(rows, nil)

		list, err := repo.ListByAccountID(t.Context(), "acc-1")
		require.NoError(t, err)
		assert.Empty(t, list)
	})

	t.Run("propagates query errors", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		boom := errors.New("query boom")
		conn.EXPECT().
			Query(mock.Anything, sqlContaining("FROM garages WHERE account_id = $1"), []any{"acc-1"}).
			Return(nil, boom)

		_, err := repo.ListByAccountID(t.Context(), "acc-1")
		require.ErrorIs(t, err, boom)
	})

	t.Run("aborts on a scan error and still closes the result set", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		boom := errors.New("scan boom")
		rows := newStubRows([][]any{
			garageScanRow("garage-1", "acc-1", "Gara A", "active"),
			garageScanRow("garage-2", "acc-1", "Gara B", "active"),
		})
		rows.scanAt, rows.scanErr = 1, boom
		conn.EXPECT().
			Query(mock.Anything, sqlContaining("FROM garages WHERE account_id = $1"), []any{"acc-1"}).
			Return(rows, nil)

		_, err := repo.ListByAccountID(t.Context(), "acc-1")
		require.ErrorIs(t, err, boom)
		require.True(t, rows.closed)
	})
}

func TestPostgresGarageRepository_Create(t *testing.T) {
	t.Run("inserts the garage and scans back id and timestamps", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		g := &domain.Garage{
			ID:        "garage-new",
			AccountID: "acc-1",
			Name:      "Gara A",
			Timezone:  "Asia/Ho_Chi_Minh",
			Currency:  "VND",
			Status:    domain.GarageStatusActive,
		}
		createdAt := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("INSERT INTO garages"), []any{
				g.ID, g.AccountID, g.Name, (*string)(nil), (*string)(nil), (*string)(nil), g.Timezone, g.Currency, "active",
			}).
			Return(&stubRow{values: []any{"garage-new", createdAt, createdAt}})

		err := repo.Create(t.Context(), g)
		require.NoError(t, err)
		assert.Equal(t, "garage-new", g.ID)
		assert.Equal(t, createdAt, g.CreatedAt)
		assert.Equal(t, createdAt, g.UpdatedAt)
	})

	t.Run("propagates scan errors", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		boom := errors.New("insert boom")
		conn.EXPECT().
			QueryRow(mock.Anything, sqlContaining("INSERT INTO garages"), mock.Anything).
			Return(&stubRow{err: boom})

		err := repo.Create(t.Context(), &domain.Garage{AccountID: "acc-1", Name: "Gara A"})
		require.ErrorIs(t, err, boom)
	})
}

func TestPostgresGarageRepository_Update(t *testing.T) {
	t.Run("updates all editable fields", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		g := &domain.Garage{
			ID: "garage-1", Name: "Gara A", Timezone: "Asia/Ho_Chi_Minh", Currency: "VND",
			Status: domain.GarageStatusActive,
		}

		conn.EXPECT().
			Exec(t.Context(), sqlContaining("UPDATE garages SET"), []any{"Gara A", (*string)(nil), (*string)(nil), (*string)(nil), "Asia/Ho_Chi_Minh", "VND", "active", "garage-1"}).
			Return(pgconn.CommandTag{}, nil)

		require.NoError(t, repo.Update(t.Context(), g))
	})

	t.Run("propagates errors", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageRepository(pool)

		boom := errors.New("update boom")
		conn.EXPECT().
			Exec(mock.Anything, sqlContaining("UPDATE garages SET"), mock.Anything).
			Return(pgconn.CommandTag{}, boom)

		err := repo.Update(t.Context(), &domain.Garage{ID: "garage-1"})
		require.ErrorIs(t, err, boom)
	})
}

// ---------------------------------------------------------------------------
// PostgresGarageStaffRepository
// ---------------------------------------------------------------------------

func TestPostgresGarageStaffRepository_Create(t *testing.T) {
	t.Run("inserts the staff member and scans back id and timestamps", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		s := &domain.GarageStaff{
			ID:        "staff-1",
			GarageID:  "garage-1",
			AccountID: "acc-2",
			Role:      domain.RoleTechnician,
			Status:    domain.StaffStatusActive,
		}
		createdAt := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("INSERT INTO garage_staff"), []any{"staff-1", "garage-1", "acc-2", "technician", "active"}).
			Return(&stubRow{values: []any{"staff-1", createdAt, createdAt}})

		require.NoError(t, repo.Create(t.Context(), s))
		assert.Equal(t, "staff-1", s.ID)
		assert.Equal(t, createdAt, s.CreatedAt)
		assert.Equal(t, createdAt, s.UpdatedAt)
	})

	t.Run("propagates scan errors", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		boom := errors.New("insert boom")
		conn.EXPECT().
			QueryRow(mock.Anything, sqlContaining("INSERT INTO garage_staff"), mock.Anything).
			Return(&stubRow{err: boom})

		err := repo.Create(t.Context(), &domain.GarageStaff{GarageID: "garage-1", AccountID: "acc-2"})
		require.ErrorIs(t, err, boom)
	})
}

func TestPostgresGarageStaffRepository_ListByGarageID(t *testing.T) {
	t.Run("returns active staff with roles and statuses mapped", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		rows := newStubRows([][]any{
			staffScanValues("staff-1", "garage-1", "acc-1", "owner", "active"),
			staffScanValues("staff-2", "garage-1", "acc-2", "service_advisor", "active"),
		})
		conn.EXPECT().
			Query(t.Context(), sqlContaining("FROM garage_staff WHERE garage_id = $1"), []any{"garage-1"}).
			Return(rows, nil)

		list, err := repo.ListByGarageID(t.Context(), "garage-1")
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, domain.RoleOwner, list[0].Role)
		assert.Equal(t, domain.RoleServiceAdvisor, list[1].Role)
		assert.Equal(t, domain.StaffStatusActive, list[0].Status)
		require.True(t, rows.closed)
	})

	t.Run("propagates query errors", func(t *testing.T) {
		conn, pool := newConnPool(t)
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
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		conn.EXPECT().
			QueryRow(t.Context(), sqlContaining("FROM garage_staff WHERE garage_id = $1 AND account_id = $2"), []any{"garage-1", "acc-2"}).
			Return(&stubRow{values: staffScanValues("staff-1", "garage-1", "acc-2", "technician", "active")})

		s, err := repo.FindByGarageAndAccount(t.Context(), "garage-1", "acc-2")
		require.NoError(t, err)
		assert.Equal(t, "garage-1", s.GarageID)
		assert.Equal(t, domain.RoleTechnician, s.Role)
		assert.Equal(t, domain.StaffStatusActive, s.Status)
	})

	t.Run("propagates errors untouched (no ErrNoRows mapping here)", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		conn.EXPECT().
			QueryRow(mock.Anything, sqlContaining("FROM garage_staff WHERE garage_id = $1 AND account_id = $2"), mock.Anything).
			Return(&stubRow{err: pgx.ErrNoRows})

		_, err := repo.FindByGarageAndAccount(t.Context(), "garage-1", "nobody")
		require.ErrorIs(t, err, pgx.ErrNoRows)
	})
}

func TestPostgresGarageStaffRepository_ListRolesForAccount(t *testing.T) {
	t.Run("returns the role assignments for every active membership", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		rows := newStubRows([][]any{
			{"garage-1", "owner"},
			{"garage-2", "accountant"},
		})
		conn.EXPECT().
			Query(t.Context(), sqlContaining("FROM garage_staff WHERE account_id = $1 AND status = 'active'"), []any{"acc-2"}).
			Return(rows, nil)

		got, err := repo.ListRolesForAccount(t.Context(), "acc-2")
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, repository.RoleAssignment{GarageID: "garage-1", Role: "owner"}, got[0])
		assert.Equal(t, repository.RoleAssignment{GarageID: "garage-2", Role: "accountant"}, got[1])
		require.True(t, rows.closed)
	})

	t.Run("propagates query errors", func(t *testing.T) {
		conn, pool := newConnPool(t)
		repo := repository.NewPostgresGarageStaffRepository(pool)

		boom := errors.New("query boom")
		conn.EXPECT().
			Query(mock.Anything, sqlContaining("FROM garage_staff WHERE account_id = $1"), []any{"acc-2"}).
			Return(nil, boom)

		_, err := repo.ListRolesForAccount(t.Context(), "acc-2")
		require.ErrorIs(t, err, boom)
	})
}
