// Package repository implements the persistence layer of the Notification
// module on top of the shared Postgres pool. See: Database Design, the
// notification_log table.
package repository

import (
	"context"
	"fmt"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresRepository implements usecase.Repository over the shared pgx
// pool.
type PostgresRepository struct {
	pool *platformdb.Pool
	tx   usecase.Transactor
}

// NewPostgresRepository returns a notification log repository backed by
// pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// WithFailureTx sets the transactor that Failed writes through. It must open
// a transaction apart from the caller's.
func (r *PostgresRepository) WithFailureTx(tx usecase.Transactor) *PostgresRepository {
	r.tx = tx

	return r
}

const insertLog = `
	INSERT INTO notification_log (id, user_id, channel, template, recipient, status, error,
	                              garage_id, customer_id, type, entity_type, entity_id, event_id, skip_reason)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

func insertArgs(log *domain.Log) []any {
	return []any{
		log.ID, nullableString(log.UserID), string(log.Channel), log.Template, log.Recipient, string(log.Status),
		nullableString(log.Error), nullableString(log.GarageID), nullableString(log.CustomerID), nullableString(string(log.Type)),
		nullableString(log.EntityType), nullableString(log.EntityID), nullableString(log.EventID), nullableString(string(log.SkipReason)),
	}
}

// Create inserts log. A row of a garage scans back its server-generated
// timestamp; an account or licence e-mail has no garage and is not readable in
// the scope that writes it, so it cannot be read back (RETURNING needs the
// row to pass the read policy).
func (r *PostgresRepository) Create(ctx context.Context, log *domain.Log) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	if log.GarageID == "" {
		_, err := q.Exec(ctx, insertLog, insertArgs(log)...)

		return err
	}

	return q.QueryRow(ctx, insertLog+" RETURNING created_at", insertArgs(log)...).Scan(&log.CreatedAt)
}

// Failed inserts log in a transaction of its own, scoped to the log's garage,
// so that it survives the rollback of the transaction that failed.
func (r *PostgresRepository) Failed(ctx context.Context, log *domain.Log) error {
	// Detached: the insert must not be part of the transaction that is about
	// to roll back.
	return r.tx.InGarage(platformdb.Detached(ctx), log.GarageID, func(ctx context.Context) error {
		return r.Create(ctx, log)
	})
}

// List returns a garage's rows, newest first, and how many match filter.
func (r *PostgresRepository) List(
	ctx context.Context, garageID string, filter usecase.LogFilter, page, pageSize int,
) ([]domain.Log, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	var total int

	if err := q.QueryRow(ctx, `
		SELECT count(*) FROM notification_log
		WHERE garage_id = $1 AND ($2 = '' OR type = $2) AND ($3 = '' OR status::text = $3)`,
		garageID, string(filter.Type), string(filter.Status)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notification log: %w", err)
	}

	rows, err := q.Query(ctx, `
		SELECT id, COALESCE(type, ''), recipient, COALESCE(customer_id::text, ''), COALESCE(entity_type, ''),
		       COALESCE(entity_id::text, ''), status::text, COALESCE(skip_reason, ''), COALESCE(error, ''), created_at
		FROM notification_log
		WHERE garage_id = $1 AND ($2 = '' OR type = $2) AND ($3 = '' OR status::text = $3)
		ORDER BY created_at DESC, id
		LIMIT $4 OFFSET $5`,
		garageID, string(filter.Type), string(filter.Status), pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("query notification log: %w", err)
	}
	defer rows.Close()

	var logs []domain.Log

	for rows.Next() {
		var (
			l                   domain.Log
			typ, status, reason string
		)

		if err := rows.Scan(&l.ID, &typ, &l.Recipient, &l.CustomerID, &l.EntityType, &l.EntityID, &status, &reason, &l.Error, &l.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan notification log: %w", err)
		}

		l.GarageID, l.Channel, l.Type = garageID, domain.ChannelEmail, domain.Type(typ)
		l.Status, l.SkipReason = domain.Status(status), domain.SkipReason(reason)
		logs = append(logs, l)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read notification log: %w", err)
	}

	return logs, total, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
