// Package repository stores the contact requests on the shared Postgres pool
// (Database Design 11.6, the contact_requests table).
package repository

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/contactrequest/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresRepository implements usecase.Repository.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const insertRequest = `
	INSERT INTO contact_requests (id, name, garage_name, phone, email, message, locale, consent_at, source_ip_hash, user_agent, status)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	RETURNING created_at`

// Create inserts r and sets its creation time.
func (r *PostgresRepository) Create(ctx context.Context, req *domain.Request) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return q.QueryRow(ctx, insertRequest,
		req.ID, req.Name, nullable(req.GarageName), req.Phone, nullable(req.Email), nullable(req.Message), req.Locale,
		req.ConsentAt, req.SourceIPHash, nullable(req.UserAgent), string(req.Status),
	).Scan(&req.CreatedAt)
}

// CountSince counts the requests of one source created at or after since.
func (r *PostgresRepository) CountSince(ctx context.Context, sourceIPHash string, since time.Time) (int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	var n int

	err := q.QueryRow(ctx, `SELECT count(*) FROM contact_requests WHERE source_ip_hash = $1 AND created_at >= $2`,
		sourceIPHash, since).Scan(&n)

	return n, err
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
