package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresAccessReader implements access.Reader: an account's access to a
// garage, read on every request (Security Architecture Section 3.2).
type PostgresAccessReader struct {
	pool *platformdb.Pool
}

// NewPostgresAccessReader returns an access reader backed by pool.
func NewPostgresAccessReader(pool *platformdb.Pool) *PostgresAccessReader {
	return &PostgresAccessReader{pool: pool}
}

// GarageAccess returns what accountID may do in garageID, or
// access.ErrNoMembership when it neither owns the garage nor is an active
// member (or cannot see it at all).
func (r *PostgresAccessReader) GarageAccess(ctx context.Context, garageID, accountID string) (*access.Access, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	var (
		ownerID     string
		active      bool
		staffStatus *string
		permissions []string
	)

	err := q.QueryRow(ctx, `
		SELECT g.account_id, g.status = 'active', s.status::text,
			COALESCE(array_agg(DISTINCT rp.permission::text) FILTER (WHERE rp.permission IS NOT NULL), '{}')
		FROM garages g
		LEFT JOIN garage_staff s ON s.garage_id = g.id AND s.account_id = $2
		LEFT JOIN staff_group_members m ON m.staff_id = s.id
		LEFT JOIN staff_groups sg ON sg.id = m.group_id
		LEFT JOIN role_permissions rp ON rp.role_id = sg.role_id
		WHERE g.id = $1
		GROUP BY g.id, s.status`, garageID, accountID,
	).Scan(&ownerID, &active, &staffStatus, &permissions)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, access.ErrNoMembership
	}

	if err != nil {
		return nil, err
	}

	result := &access.Access{
		GarageID: garageID, AccountID: accountID, OwnerAccountID: ownerID,
		Owner: ownerID == accountID, Active: active,
	}

	if result.Owner {
		return result, nil
	}

	if staffStatus == nil || *staffStatus != "active" {
		return nil, access.ErrNoMembership
	}

	result.Permissions = toPermissions(permissions)

	return result, nil
}

// OwnerUsage counts the owner's active garages and staff (distinct
// non-owner members and live pending invitees) across all their garages,
// through the owner_usage() function (it sees past the caller's RLS scope).
func (r *PostgresAccessReader) OwnerUsage(ctx context.Context, ownerAccountID string) (garages, staff int, err error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	err = q.QueryRow(ctx, `SELECT garages, staff FROM owner_usage($1)`, ownerAccountID).Scan(&garages, &staff)

	return garages, staff, err
}
