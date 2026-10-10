package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// PostgresRoleRepository implements usecase.RoleRepository.
type PostgresRoleRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRoleRepository returns a role repository backed by pool.
func NewPostgresRoleRepository(pool *platformdb.Pool) *PostgresRoleRepository {
	return &PostgresRoleRepository{pool: pool}
}

const roleSelect = `
	SELECT r.id, r.owner_user_id, r.name, r.description, r.default_key::text,
		COALESCE(array_agg(rp.permission::text) FILTER (WHERE rp.permission IS NOT NULL), '{}'),
		(SELECT count(*) FROM staff_groups sg WHERE sg.role_id = r.id)::int,
		r.created_at, r.updated_at
	FROM roles r
	LEFT JOIN role_permissions rp ON rp.role_id = r.id`

func scanRole(row pgx.Row) (*domain.Role, error) {
	var (
		role        domain.Role
		defaultKey  *string
		permissions []string
	)

	err := row.Scan(&role.ID, &role.OwnerUserID, &role.Name, &role.Description, &defaultKey, &permissions,
		&role.GroupCount, &role.CreatedAt, &role.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRoleNotFound
		}

		return nil, err
	}

	if defaultKey != nil {
		key := domain.DefaultRole(*defaultKey)
		role.DefaultKey = &key
	}

	role.Permissions = toPermissions(permissions)

	return &role, nil
}

// ListByOwner returns the owner's roles, default roles first.
func (r *PostgresRoleRepository) ListByOwner(ctx context.Context, ownerUserID string) ([]*domain.Role, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, roleSelect+` WHERE r.owner_user_id = $1 GROUP BY r.id
		ORDER BY r.default_key IS NULL, r.default_key, lower(r.name)`, ownerUserID)
	if err != nil {
		return nil, err
	}

	return collect(rows, scanRole)
}

// FindByID returns a role visible in the current scope.
func (r *PostgresRoleRepository) FindByID(ctx context.Context, roleID string) (*domain.Role, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanRole(q.QueryRow(ctx, roleSelect+` WHERE r.id = $1 GROUP BY r.id`, roleID))
}

// Create inserts a role and its permissions.
func (r *PostgresRoleRepository) Create(ctx context.Context, role *domain.Role) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO roles (owner_user_id, name, description) VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at`,
		role.OwnerUserID, role.Name, role.Description,
	).Scan(&role.ID, &role.CreatedAt, &role.UpdatedAt)
	if isUniqueViolation(err, "") {
		return domain.ErrRoleNameTaken
	}

	if err != nil {
		return err
	}

	return r.setPermissions(ctx, role.ID, role.Permissions)
}

// CreateDefault inserts a default role unless one with its key (or name)
// exists.
func (r *PostgresRoleRepository) CreateDefault(ctx context.Context, role *domain.Role) (bool, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO roles (owner_user_id, name, description, default_key) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
		RETURNING id, created_at, updated_at`,
		role.OwnerUserID, role.Name, role.Description, string(*role.DefaultKey),
	).Scan(&role.ID, &role.CreatedAt, &role.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, r.setPermissions(ctx, role.ID, role.Permissions)
}

// Update persists name, description and permissions.
func (r *PostgresRoleRepository) Update(ctx context.Context, role *domain.Role) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `UPDATE roles SET name = $2, description = $3 WHERE id = $1 RETURNING updated_at`,
		role.ID, role.Name, role.Description).Scan(&role.UpdatedAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrRoleNotFound
	case isUniqueViolation(err, ""):
		return domain.ErrRoleNameTaken
	case err != nil:
		return err
	}

	if _, err := q.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, role.ID); err != nil {
		return err
	}

	return r.setPermissions(ctx, role.ID, role.Permissions)
}

// Delete removes a role no group uses.
func (r *PostgresRoleRepository) Delete(ctx context.Context, roleID string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	tag, err := q.Exec(ctx, `DELETE FROM roles WHERE id = $1`, roleID)
	if isForeignKeyViolation(err) {
		return domain.ErrRoleInUse
	}

	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrRoleNotFound
	}

	return nil
}

func (r *PostgresRoleRepository) setPermissions(ctx context.Context, roleID string, perms []access.Permission) error {
	if len(perms) == 0 {
		return nil
	}

	values := make([]string, 0, len(perms))
	for _, p := range perms {
		values = append(values, string(p))
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)
	_, err := q.Exec(ctx, `INSERT INTO role_permissions (role_id, permission) SELECT $1, unnest($2::permission[])`, roleID, values)

	return err
}
