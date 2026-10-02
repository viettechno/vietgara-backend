// Package repository implements the persistence layer of the Notification
// module on top of the shared Postgres pool. See: Database Design, the
// notification_log table (migration 000019).
package repository

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresRepository implements usecase.Repository over the shared pgx
// pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a notification log repository backed by
// pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create inserts log, scanning back its server-generated timestamp.
func (r *PostgresRepository) Create(ctx context.Context, log *domain.Log) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	row := q.QueryRow(ctx, `
		INSERT INTO notification_log (id, account_id, channel, template, recipient, status, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at`,
		log.ID, nullableString(log.AccountID), string(log.Channel), log.Template, log.Recipient, string(log.Status),
		nullableString(log.Error),
	)

	return row.Scan(&log.CreatedAt)
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
