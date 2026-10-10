package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// PostgresStaffRepository implements usecase.StaffRepository.
type PostgresStaffRepository struct {
	pool *platformdb.Pool
}

// NewPostgresStaffRepository returns a staff repository backed by pool.
func NewPostgresStaffRepository(pool *platformdb.Pool) *PostgresStaffRepository {
	return &PostgresStaffRepository{pool: pool}
}

// staffSelect loads memberships with their groups and the union of their
// groups' role permissions.
const staffSelect = `
	SELECT s.id, s.garage_id, s.user_id, s.status, s.user_id = g.user_id,
		COALESCE(array_agg(DISTINCT m.group_id::text) FILTER (WHERE m.group_id IS NOT NULL), '{}'),
		COALESCE(array_agg(DISTINCT rp.permission::text) FILTER (WHERE rp.permission IS NOT NULL), '{}'),
		s.created_at, s.updated_at
	FROM garage_staff s
	JOIN garages g ON g.id = s.garage_id
	LEFT JOIN staff_group_members m ON m.staff_id = s.id
	LEFT JOIN staff_groups sg ON sg.id = m.group_id
	LEFT JOIN role_permissions rp ON rp.role_id = sg.role_id`

const staffGroupBy = ` GROUP BY s.id, g.user_id`

func scanStaff(row pgx.Row) (*domain.Staff, error) {
	var (
		s           domain.Staff
		status      string
		permissions []string
	)

	err := row.Scan(&s.ID, &s.GarageID, &s.UserID, &status, &s.Owner, &s.GroupIDs, &permissions, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrStaffNotFound
		}

		return nil, err
	}

	s.Status = domain.StaffStatus(status)
	s.Permissions = toPermissions(permissions)

	return &s, nil
}

func toPermissions(values []string) []access.Permission {
	perms := make([]access.Permission, 0, len(values))
	for _, v := range values {
		perms = append(perms, access.Permission(v))
	}

	return access.Normalize(perms)
}

// FindByID returns one membership of the garage.
func (r *PostgresStaffRepository) FindByID(ctx context.Context, garageID, staffID string) (*domain.Staff, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanStaff(q.QueryRow(ctx, staffSelect+` WHERE s.garage_id = $1 AND s.id = $2`+staffGroupBy, garageID, staffID))
}

// FindByUser returns the user's membership of the garage.
func (r *PostgresStaffRepository) FindByUser(ctx context.Context, garageID, userID string) (*domain.Staff, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanStaff(q.QueryRow(ctx, staffSelect+` WHERE s.garage_id = $1 AND s.user_id = $2`+staffGroupBy, garageID, userID))
}

// List returns the garage's members, owner first.
func (r *PostgresStaffRepository) List(ctx context.Context, garageID string) ([]*domain.Staff, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, staffSelect+` WHERE s.garage_id = $1`+staffGroupBy+` ORDER BY s.user_id = g.user_id DESC, s.created_at`, garageID)
	if err != nil {
		return nil, err
	}

	return collect(rows, scanStaff)
}

// Create inserts a membership; a second membership of the same user
// yields domain.ErrAlreadyMember.
func (r *PostgresStaffRepository) Create(ctx context.Context, s *domain.Staff) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO garage_staff (garage_id, user_id, status) VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at`,
		s.GarageID, s.UserID, string(s.Status),
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if isUniqueViolation(err, "") {
		return domain.ErrAlreadyMember
	}

	return err
}

// UpdateStatus persists s.Status.
func (r *PostgresStaffRepository) UpdateStatus(ctx context.Context, s *domain.Staff) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `UPDATE garage_staff SET status = $3 WHERE garage_id = $1 AND id = $2 RETURNING updated_at`,
		s.GarageID, s.ID, string(s.Status)).Scan(&s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrStaffNotFound
	}

	return err
}

// SetGroups replaces the member's groups.
func (r *PostgresStaffRepository) SetGroups(ctx context.Context, garageID, staffID string, groupIDs []string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	if _, err := q.Exec(ctx, `DELETE FROM staff_group_members WHERE garage_id = $1 AND staff_id = $2`, garageID, staffID); err != nil {
		return err
	}

	if len(groupIDs) == 0 {
		return nil
	}

	_, err := q.Exec(ctx, `
		INSERT INTO staff_group_members (group_id, staff_id, garage_id)
		SELECT unnest($3::uuid[]), $2, $1`, garageID, staffID, groupIDs)

	return err
}

// Delete removes a membership (and its group memberships).
func (r *PostgresStaffRepository) Delete(ctx context.Context, garageID, staffID string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	tag, err := q.Exec(ctx, `DELETE FROM garage_staff WHERE garage_id = $1 AND id = $2`, garageID, staffID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrStaffNotFound
	}

	return nil
}
