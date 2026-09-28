package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// PostgresGroupRepository implements usecase.GroupRepository.
type PostgresGroupRepository struct {
	pool *platformdb.Pool
}

// NewPostgresGroupRepository returns a staff group repository backed by pool.
func NewPostgresGroupRepository(pool *platformdb.Pool) *PostgresGroupRepository {
	return &PostgresGroupRepository{pool: pool}
}

const groupSelect = `
	SELECT sg.id, sg.garage_id, sg.name, sg.description, sg.role_id, r.name, sg.default_key::text,
		(SELECT count(*) FROM staff_group_members m WHERE m.group_id = sg.id)::int,
		sg.created_at, sg.updated_at
	FROM staff_groups sg
	JOIN roles r ON r.id = sg.role_id`

func scanGroup(row pgx.Row) (*domain.StaffGroup, error) {
	var (
		g          domain.StaffGroup
		defaultKey *string
	)

	err := row.Scan(&g.ID, &g.GarageID, &g.Name, &g.Description, &g.RoleID, &g.RoleName, &defaultKey,
		&g.MemberCount, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrGroupNotFound
		}

		return nil, err
	}

	if defaultKey != nil {
		key := domain.DefaultRole(*defaultKey)
		g.DefaultKey = &key
	}

	return &g, nil
}

// List returns the garage's groups, default groups first.
func (r *PostgresGroupRepository) List(ctx context.Context, garageID string) ([]*domain.StaffGroup, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, groupSelect+` WHERE sg.garage_id = $1
		ORDER BY sg.default_key IS NULL, sg.default_key, lower(sg.name)`, garageID)
	if err != nil {
		return nil, err
	}

	return collect(rows, scanGroup)
}

// FindByID returns one group of the garage.
func (r *PostgresGroupRepository) FindByID(ctx context.Context, garageID, groupID string) (*domain.StaffGroup, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanGroup(q.QueryRow(ctx, groupSelect+` WHERE sg.garage_id = $1 AND sg.id = $2`, garageID, groupID))
}

// Create inserts a group.
func (r *PostgresGroupRepository) Create(ctx context.Context, g *domain.StaffGroup) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO staff_groups (garage_id, name, description, role_id) VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`,
		g.GarageID, g.Name, g.Description, g.RoleID,
	).Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt)
	if isUniqueViolation(err, "") {
		return domain.ErrGroupNameTaken
	}

	return err
}

// CreateDefault inserts a default group unless the garage has it (or a
// group of that name) already.
func (r *PostgresGroupRepository) CreateDefault(ctx context.Context, g *domain.StaffGroup) (bool, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO staff_groups (garage_id, name, description, role_id, default_key) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING
		RETURNING id, created_at, updated_at`,
		g.GarageID, g.Name, g.Description, g.RoleID, string(*g.DefaultKey),
	).Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	return err == nil, err
}

// Update persists name, description and role.
func (r *PostgresGroupRepository) Update(ctx context.Context, g *domain.StaffGroup) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		UPDATE staff_groups SET name = $3, description = $4, role_id = $5 WHERE garage_id = $1 AND id = $2
		RETURNING updated_at`,
		g.GarageID, g.ID, g.Name, g.Description, g.RoleID,
	).Scan(&g.UpdatedAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrGroupNotFound
	case isUniqueViolation(err, ""):
		return domain.ErrGroupNameTaken
	default:
		return err
	}
}

// Delete removes a group and its memberships.
func (r *PostgresGroupRepository) Delete(ctx context.Context, garageID, groupID string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	tag, err := q.Exec(ctx, `DELETE FROM staff_groups WHERE garage_id = $1 AND id = $2`, garageID, groupID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrGroupNotFound
	}

	return nil
}
