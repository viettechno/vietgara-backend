package repository

import (
	"context"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

// PostgresGarageStaffRepository implements GarageStaffRepository over the
// shared pgx pool.
type PostgresGarageStaffRepository struct {
	pool *platformdb.Pool
}

// NewPostgresGarageStaffRepository returns a staff repository backed by pool.
func NewPostgresGarageStaffRepository(pool *platformdb.Pool) *PostgresGarageStaffRepository {
	return &PostgresGarageStaffRepository{pool: pool}
}

// Create inserts s and scans back the server-generated timestamps.
func (r *PostgresGarageStaffRepository) Create(ctx context.Context, s *domain.GarageStaff) error {
	// s.ID is generated call-site side (see usecase.CreateGarage) so the
	// garage-scoped RLS WITH CHECK (garage_id = current_garage_id) and later
	// lookups by id work with the caller-chosen id.
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `
		INSERT INTO garage_staff (id, garage_id, account_id, role, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`,
		s.ID, s.GarageID, s.AccountID, string(s.Role), string(s.Status),
	)

	return row.Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
}

// ListByGarageID returns the active staff members of garageID, ordered by
// creation time.
func (r *PostgresGarageStaffRepository) ListByGarageID(ctx context.Context, garageID string) ([]*domain.GarageStaff, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `SELECT id, garage_id, account_id, role, status, created_at, updated_at
		FROM garage_staff WHERE garage_id = $1 AND status = 'active' ORDER BY created_at ASC`, garageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*domain.GarageStaff

	for rows.Next() {
		var (
			s            domain.GarageStaff
			role, status string
		)
		if err := rows.Scan(&s.ID, &s.GarageID, &s.AccountID, &role, &status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}

		s.Role, s.Status = domain.Role(role), domain.StaffStatus(status)
		result = append(result, &s)
	}

	return result, rows.Err()
}

// FindByID returns the staff record with the given id, or pgx.ErrNoRows
// when absent.
func (r *PostgresGarageStaffRepository) FindByID(ctx context.Context, staffID string) (*domain.GarageStaff, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT id, garage_id, account_id, role, status, created_at, updated_at
		FROM garage_staff WHERE id = $1`, staffID)

	var (
		s            domain.GarageStaff
		role, status string
	)

	if err := row.Scan(&s.ID, &s.GarageID, &s.AccountID, &role, &status, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}

	s.Role, s.Status = domain.Role(role), domain.StaffStatus(status)

	return &s, nil
}

// Update persists the role and status of s.
func (r *PostgresGarageStaffRepository) Update(ctx context.Context, s *domain.GarageStaff) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE garage_staff SET role=$1, status=$2, updated_at=now()
		WHERE id = $3`,
		string(s.Role), string(s.Status), s.ID,
	)

	return err
}

// FindByGarageAndAccount returns the staff record matching garageID and
// accountID, or the underlying pgx error (e.g. pgx.ErrNoRows) when absent.
func (r *PostgresGarageStaffRepository) FindByGarageAndAccount(ctx context.Context, garageID, accountID string) (*domain.GarageStaff, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT id, garage_id, account_id, role, status, created_at, updated_at
		FROM garage_staff WHERE garage_id = $1 AND account_id = $2`, garageID, accountID)

	var (
		s            domain.GarageStaff
		role, status string
	)

	err := row.Scan(&s.ID, &s.GarageID, &s.AccountID, &role, &status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}

	s.Role, s.Status = domain.Role(role), domain.StaffStatus(status)

	return &s, nil
}

// ListRolesForAccount deliberately queries WITHOUT relying on the
// garage-scoped RLS session variable (like ListByAccountID above) --
// listing "every garage this account has a role in" is used at login
// time, before any single garage_id is known yet.
func (r *PostgresGarageStaffRepository) ListRolesForAccount(ctx context.Context, accountID string) ([]usecase.RoleAssignment, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `SELECT garage_id, role FROM garage_staff WHERE account_id = $1 AND status = 'active'`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []usecase.RoleAssignment

	for rows.Next() {
		var ra usecase.RoleAssignment
		if err := rows.Scan(&ra.GarageID, &ra.Role); err != nil {
			return nil, err
		}

		result = append(result, ra)
	}

	return result, rows.Err()
}
