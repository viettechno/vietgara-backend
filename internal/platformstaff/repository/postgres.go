// Package repository stores the platform staff model in PostgreSQL
// (migration 000033). The tables belong to no garage and have no row-level
// security; the platform permission interceptor protects the RPCs.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
)

// Postgres implements usecase.Repository and access.PlatformReader.
type Postgres struct {
	pool *platformdb.Pool
}

// NewPostgres returns a repository backed by pool.
func NewPostgres(pool *platformdb.Pool) *Postgres { return &Postgres{pool: pool} }

func (r *Postgres) q(ctx context.Context) platformdb.Querier {
	return platformdb.QuerierFromContext(ctx, r.pool)
}

// --- access ---

// PlatformAccess returns the caller's platform access: Admin when listed in
// platform_admins, otherwise the union of the roles of their groups. A user
// who is not active has none.
func (r *Postgres) PlatformAccess(ctx context.Context, userID string) (*access.PlatformAccess, error) {
	acc := &access.PlatformAccess{UserID: userID}

	var permissions []string

	err := r.q(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM platform_admins pa WHERE pa.user_id = u.id),
			COALESCE((
				SELECT array_agg(DISTINCT rp.permission::text)
				FROM platform_staff_group_members m
				JOIN platform_staff_groups g ON g.id = m.group_id
				JOIN platform_role_permissions rp ON rp.role_id = g.role_id
				WHERE m.user_id = u.id), '{}')
		FROM users u
		WHERE u.id = $1 AND u.status = 'active'`, userID,
	).Scan(&acc.Admin, &permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return acc, nil
	}

	if err != nil {
		return nil, err
	}

	acc.Permissions = toPermissions(permissions)

	return acc, nil
}

func toPermissions(values []string) []access.PlatformPermission {
	perms := make([]access.PlatformPermission, 0, len(values))
	for _, v := range values {
		perms = append(perms, access.PlatformPermission(v))
	}

	return access.NormalizePlatform(perms)
}

// --- roles ---

const roleSelect = `
	SELECT r.id, r.name, r.description,
		COALESCE(array_agg(rp.permission::text) FILTER (WHERE rp.permission IS NOT NULL), '{}'),
		(SELECT count(*) FROM platform_staff_groups g WHERE g.role_id = r.id)::int,
		r.created_at, r.updated_at
	FROM platform_roles r
	LEFT JOIN platform_role_permissions rp ON rp.role_id = r.id`

func scanRole(row pgx.Row) (*domain.Role, error) {
	var (
		role        domain.Role
		permissions []string
	)

	err := row.Scan(&role.ID, &role.Name, &role.Description, &permissions, &role.GroupCount, &role.CreatedAt, &role.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrRoleNotFound
	}

	if err != nil {
		return nil, err
	}

	role.Permissions = toPermissions(permissions)

	return &role, nil
}

// ListRoles returns every role by name.
func (r *Postgres) ListRoles(ctx context.Context) ([]*domain.Role, error) {
	rows, err := r.q(ctx).Query(ctx, roleSelect+` GROUP BY r.id ORDER BY lower(r.name)`)
	if err != nil {
		return nil, err
	}

	return collect(rows, scanRole)
}

// FindRole returns one role.
func (r *Postgres) FindRole(ctx context.Context, id string) (*domain.Role, error) {
	return scanRole(r.q(ctx).QueryRow(ctx, roleSelect+` WHERE r.id = $1 GROUP BY r.id`, id))
}

// CreateRole inserts a role and its permissions.
func (r *Postgres) CreateRole(ctx context.Context, role *domain.Role) error {
	err := r.q(ctx).QueryRow(ctx, `
		INSERT INTO platform_roles (name, description) VALUES ($1, $2)
		RETURNING id, created_at, updated_at`, role.Name, role.Description,
	).Scan(&role.ID, &role.CreatedAt, &role.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.ErrRoleNameTaken
	}

	if err != nil {
		return err
	}

	return r.setPermissions(ctx, role.ID, role.Permissions)
}

// UpdateRole persists name, description and permissions.
func (r *Postgres) UpdateRole(ctx context.Context, role *domain.Role) error {
	err := r.q(ctx).QueryRow(ctx, `UPDATE platform_roles SET name = $2, description = $3 WHERE id = $1 RETURNING updated_at`,
		role.ID, role.Name, role.Description).Scan(&role.UpdatedAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrRoleNotFound
	case isUniqueViolation(err):
		return domain.ErrRoleNameTaken
	case err != nil:
		return err
	}

	if _, err := r.q(ctx).Exec(ctx, `DELETE FROM platform_role_permissions WHERE role_id = $1`, role.ID); err != nil {
		return err
	}

	return r.setPermissions(ctx, role.ID, role.Permissions)
}

// DeleteRole removes a role no group uses.
func (r *Postgres) DeleteRole(ctx context.Context, id string) error {
	tag, err := r.q(ctx).Exec(ctx, `DELETE FROM platform_roles WHERE id = $1`, id)
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

func (r *Postgres) setPermissions(ctx context.Context, roleID string, perms []access.PlatformPermission) error {
	if len(perms) == 0 {
		return nil
	}

	values := make([]string, 0, len(perms))
	for _, p := range perms {
		values = append(values, string(p))
	}

	_, err := r.q(ctx).Exec(ctx,
		`INSERT INTO platform_role_permissions (role_id, permission) SELECT $1, unnest($2::platform_permission[])`, roleID, values)

	return err
}

// --- groups ---

const groupSelect = `
	SELECT g.id, g.name, g.description, g.role_id, r.name,
		(SELECT count(*) FROM platform_staff_group_members m WHERE m.group_id = g.id)::int,
		g.created_at, g.updated_at
	FROM platform_staff_groups g
	JOIN platform_roles r ON r.id = g.role_id`

func scanGroup(row pgx.Row) (*domain.Group, error) {
	var g domain.Group

	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.RoleID, &g.RoleName, &g.MemberCount, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrGroupNotFound
	}

	if err != nil {
		return nil, err
	}

	return &g, nil
}

// ListGroups returns every group by name.
func (r *Postgres) ListGroups(ctx context.Context) ([]*domain.Group, error) {
	rows, err := r.q(ctx).Query(ctx, groupSelect+` ORDER BY lower(g.name)`)
	if err != nil {
		return nil, err
	}

	return collect(rows, scanGroup)
}

// FindGroup returns one group.
func (r *Postgres) FindGroup(ctx context.Context, id string) (*domain.Group, error) {
	return scanGroup(r.q(ctx).QueryRow(ctx, groupSelect+` WHERE g.id = $1`, id))
}

// CreateGroup inserts a group.
func (r *Postgres) CreateGroup(ctx context.Context, g *domain.Group) error {
	err := r.q(ctx).QueryRow(ctx, `
		INSERT INTO platform_staff_groups (name, description, role_id) VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at`, g.Name, g.Description, g.RoleID,
	).Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.ErrGroupNameTaken
	}

	return err
}

// UpdateGroup persists name, description and role.
func (r *Postgres) UpdateGroup(ctx context.Context, g *domain.Group) error {
	err := r.q(ctx).QueryRow(ctx, `
		UPDATE platform_staff_groups SET name = $2, description = $3, role_id = $4 WHERE id = $1 RETURNING updated_at`,
		g.ID, g.Name, g.Description, g.RoleID).Scan(&g.UpdatedAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.ErrGroupNotFound
	case isUniqueViolation(err):
		return domain.ErrGroupNameTaken
	}

	return err
}

// DeleteGroup removes the group; its members and invitations go with it.
func (r *Postgres) DeleteGroup(ctx context.Context, id string) error {
	tag, err := r.q(ctx).Exec(ctx, `DELETE FROM platform_staff_groups WHERE id = $1`, id)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrGroupNotFound
	}

	return nil
}

// --- members ---

// GroupMembers returns the user ids of a group, oldest first.
func (r *Postgres) GroupMembers(ctx context.Context, groupID string) ([]string, error) {
	rows, err := r.q(ctx).Query(ctx,
		`SELECT user_id FROM platform_staff_group_members WHERE group_id = $1 ORDER BY created_at, user_id`, groupID)
	if err != nil {
		return nil, err
	}

	return collect(rows, func(row pgx.Row) (string, error) {
		var id string

		return id, row.Scan(&id)
	})
}

// Memberships returns, for every member, the groups they are in.
func (r *Postgres) Memberships(ctx context.Context) (map[string][]domain.GroupRef, error) {
	rows, err := r.q(ctx).Query(ctx, `
		SELECT m.user_id, g.id, g.name
		FROM platform_staff_group_members m
		JOIN platform_staff_groups g ON g.id = m.group_id
		ORDER BY lower(g.name)`)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	out := map[string][]domain.GroupRef{}

	for rows.Next() {
		var (
			userID string
			ref    domain.GroupRef
		)

		if err := rows.Scan(&userID, &ref.ID, &ref.Name); err != nil {
			return nil, err
		}

		out[userID] = append(out[userID], ref)
	}

	return out, rows.Err()
}

// AdminIDs returns the Platform Admins' user ids.
func (r *Postgres) AdminIDs(ctx context.Context) ([]string, error) {
	rows, err := r.q(ctx).Query(ctx, `SELECT user_id FROM platform_admins ORDER BY created_at, user_id`)
	if err != nil {
		return nil, err
	}

	return collect(rows, func(row pgx.Row) (string, error) {
		var id string

		return id, row.Scan(&id)
	})
}

// IsAdmin reports whether the user is a Platform Admin.
func (r *Postgres) IsAdmin(ctx context.Context, userID string) (bool, error) {
	var admin bool

	err := r.q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform_admins WHERE user_id = $1)`, userID).Scan(&admin)

	return admin, err
}

// AddMember puts a user in a group (idempotent).
func (r *Postgres) AddMember(ctx context.Context, groupID, userID string) error {
	_, err := r.q(ctx).Exec(ctx,
		`INSERT INTO platform_staff_group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, groupID, userID)

	return err
}

// RemoveMember takes a user out of a group.
func (r *Postgres) RemoveMember(ctx context.Context, groupID, userID string) error {
	tag, err := r.q(ctx).Exec(ctx,
		`DELETE FROM platform_staff_group_members WHERE group_id = $1 AND user_id = $2`, groupID, userID)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrMemberNotFound
	}

	return nil
}

// IsMember reports whether the user is in the group.
func (r *Postgres) IsMember(ctx context.Context, groupID, userID string) (bool, error) {
	var member bool

	err := r.q(ctx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM platform_staff_group_members WHERE group_id = $1 AND user_id = $2)`, groupID, userID).Scan(&member)

	return member, err
}

// --- invitations ---

const invitationSelect = `
	SELECT i.id, i.group_id, g.name, r.name, i.user_id, i.status::text, i.invited_by,
		i.expires_at, i.responded_at, i.created_at, i.updated_at
	FROM platform_staff_invitations i
	JOIN platform_staff_groups g ON g.id = i.group_id
	JOIN platform_roles r ON r.id = g.role_id`

func scanInvitation(row pgx.Row) (*domain.Invitation, error) {
	var (
		inv    domain.Invitation
		status string
	)

	err := row.Scan(&inv.ID, &inv.GroupID, &inv.GroupName, &inv.RoleName, &inv.UserID, &status, &inv.InvitedBy,
		&inv.ExpiresAt, &inv.RespondedAt, &inv.CreatedAt, &inv.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrInvitationNotFound
	}

	if err != nil {
		return nil, err
	}

	inv.Status = domain.InvitationStatus(status)

	return &inv, nil
}

// CreateInvitation inserts a pending invitation; a second pending one for
// the same user and group is domain.ErrAlreadyInvited.
func (r *Postgres) CreateInvitation(ctx context.Context, inv *domain.Invitation) error {
	err := r.q(ctx).QueryRow(ctx, `
		INSERT INTO platform_staff_invitations (group_id, user_id, status, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at, updated_at`,
		inv.GroupID, inv.UserID, string(inv.Status), inv.InvitedBy, inv.ExpiresAt,
	).Scan(&inv.ID, &inv.CreatedAt, &inv.UpdatedAt)
	if isUniqueViolation(err) {
		return domain.ErrAlreadyInvited
	}

	return err
}

// FindInvitation returns one invitation.
func (r *Postgres) FindInvitation(ctx context.Context, id string) (*domain.Invitation, error) {
	return scanInvitation(r.q(ctx).QueryRow(ctx, invitationSelect+` WHERE i.id = $1`, id))
}

// UpdateInvitation persists the status and the response time.
func (r *Postgres) UpdateInvitation(ctx context.Context, inv *domain.Invitation) error {
	tag, err := r.q(ctx).Exec(ctx,
		`UPDATE platform_staff_invitations SET status = $2, responded_at = $3 WHERE id = $1`,
		inv.ID, string(inv.Status), inv.RespondedAt)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrInvitationNotFound
	}

	return nil
}

// ListInvitations returns every invitation, newest first.
func (r *Postgres) ListInvitations(ctx context.Context) ([]*domain.Invitation, error) {
	rows, err := r.q(ctx).Query(ctx, invitationSelect+` ORDER BY i.created_at DESC`)
	if err != nil {
		return nil, err
	}

	return collect(rows, scanInvitation)
}

// ListInvitationsByUser returns the invitations sent to a user, newest first.
func (r *Postgres) ListInvitationsByUser(ctx context.Context, userID string) ([]*domain.Invitation, error) {
	rows, err := r.q(ctx).Query(ctx, invitationSelect+` WHERE i.user_id = $1 ORDER BY i.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}

	return collect(rows, scanInvitation)
}

// ExpireStale marks the user's pending invitation to the group as expired
// when it is past its expiry, so a new one can be made.
func (r *Postgres) ExpireStale(ctx context.Context, groupID, userID string, now time.Time) error {
	_, err := r.q(ctx).Exec(ctx, `
		UPDATE platform_staff_invitations SET status = 'expired'
		WHERE group_id = $1 AND user_id = $2 AND status = 'pending' AND expires_at <= $3`, groupID, userID, now)

	return err
}

// --- helpers ---

func collect[T any](rows pgx.Rows, scan func(pgx.Row) (T, error)) ([]T, error) {
	defer rows.Close()

	var out []T

	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, item)
	}

	return out, rows.Err()
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isForeignKeyViolation reports a foreign-key error: 23503, or 23001 when the
// key is ON DELETE RESTRICT.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && (pgErr.Code == "23503" || pgErr.Code == "23001")
}

// HasPlatformAccess reports whether the user has any platform access: it
// lets Identity audit the sign-ins of platform users without knowing the
// permission model.
func (r *Postgres) HasPlatformAccess(ctx context.Context, userID string) (bool, error) {
	acc, err := r.PlatformAccess(ctx, userID)
	if err != nil {
		return false, err
	}

	return acc.Any(), nil
}
