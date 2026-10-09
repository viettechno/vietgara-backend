package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// PostgresInvitationRepository implements usecase.InvitationRepository.
type PostgresInvitationRepository struct {
	pool *platformdb.Pool
}

// NewPostgresInvitationRepository returns an invitation repository backed
// by pool.
func NewPostgresInvitationRepository(pool *platformdb.Pool) *PostgresInvitationRepository {
	return &PostgresInvitationRepository{pool: pool}
}

const invitationSelect = `
	SELECT i.id, i.garage_id, i.user_id, i.group_ids::text[], i.status, i.invited_by, i.expires_at, i.responded_at,
		i.created_at, i.updated_at, COALESCE(g.name, '')
	FROM staff_invitations i
	LEFT JOIN garages g ON g.id = i.garage_id`

func scanInvitation(row pgx.Row) (*domain.Invitation, error) {
	var (
		inv    domain.Invitation
		status string
	)

	err := row.Scan(&inv.ID, &inv.GarageID, &inv.UserID, &inv.GroupIDs, &status, &inv.InvitedBy, &inv.ExpiresAt,
		&inv.RespondedAt, &inv.CreatedAt, &inv.UpdatedAt, &inv.GarageName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvitationNotFound
		}

		return nil, err
	}

	inv.Status = domain.InvitationStatus(status)

	return &inv, nil
}

func (r *PostgresInvitationRepository) list(ctx context.Context, where, arg string) ([]*domain.Invitation, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, invitationSelect+` WHERE `+where+` ORDER BY i.created_at DESC`, arg)
	if err != nil {
		return nil, err
	}

	return collect(rows, scanInvitation)
}

// FindByID returns an invitation visible to the garage or the invitee.
func (r *PostgresInvitationRepository) FindByID(ctx context.Context, id string) (*domain.Invitation, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanInvitation(q.QueryRow(ctx, invitationSelect+` WHERE i.id = $1`, id))
}

// ListByGarage returns the garage's invitations, newest first.
func (r *PostgresInvitationRepository) ListByGarage(ctx context.Context, garageID string) ([]*domain.Invitation, error) {
	return r.list(ctx, `i.garage_id = $1`, garageID)
}

// ListByUser returns the invitations sent to a user, newest first.
func (r *PostgresInvitationRepository) ListByUser(ctx context.Context, userID string) ([]*domain.Invitation, error) {
	return r.list(ctx, `i.user_id = $1`, userID)
}

// Create inserts a pending invitation.
func (r *PostgresInvitationRepository) Create(ctx context.Context, inv *domain.Invitation) error {
	groupIDs := inv.GroupIDs
	if groupIDs == nil {
		groupIDs = []string{}
	}

	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO staff_invitations (garage_id, user_id, group_ids, status, invited_by, expires_at)
		VALUES ($1, $2, $3::uuid[], $4, $5, $6)
		RETURNING id, created_at, updated_at`,
		inv.GarageID, inv.UserID, groupIDs, string(inv.Status), inv.InvitedBy, inv.ExpiresAt,
	).Scan(&inv.ID, &inv.CreatedAt, &inv.UpdatedAt)
	if isUniqueViolation(err, "") {
		return domain.ErrAlreadyInvited
	}

	return err
}

// Update persists status and responded_at.
func (r *PostgresInvitationRepository) Update(ctx context.Context, inv *domain.Invitation) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `UPDATE staff_invitations SET status = $2, responded_at = $3 WHERE id = $1 RETURNING updated_at`,
		inv.ID, string(inv.Status), inv.RespondedAt).Scan(&inv.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrInvitationNotFound
	}

	return err
}

// ExpireStale marks lapsed pending invitations as expired.
func (r *PostgresInvitationRepository) ExpireStale(ctx context.Context, garageID, userID string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE staff_invitations SET status = 'expired'
		WHERE garage_id = $1 AND user_id = $2 AND status = 'pending' AND expires_at <= now()`, garageID, userID)

	return err
}
