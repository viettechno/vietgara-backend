// Package repository stores the data requests on the shared Postgres pool.
// The requests of a garage are row-level secured; the requests that wait for
// their link belong to no garage.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
	"github.com/viettechno/vietgara-backend/internal/datarequest/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresRepository implements usecase.Repository and usecase.Confirmations.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a repository over pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const requestColumns = `id::text, garage_id::text, customer_id::text, type::text, status::text, source::text, details, outcome,
	requested_by::text, requested_at, completed_at, completed_by::text`

func scanRequest(row pgx.Row, extra ...any) (*domain.Request, error) {
	var (
		r                      domain.Request
		typ, status, sourceTxt string
	)

	dest := append([]any{
		&r.ID, &r.GarageID, &r.CustomerID, &typ, &status, &sourceTxt, &r.Details, &r.Outcome,
		&r.RequestedBy, &r.RequestedAt, &r.CompletedAt, &r.CompletedBy,
	}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRequestNotFound
		}

		return nil, err
	}

	r.Type, r.Status, r.Source = domain.Type(typ), domain.Status(status), domain.Source(sourceTxt)

	return &r, nil
}

func isUUID(s string) bool { return uuid.Validate(s) == nil }

// Create inserts a request.
func (r *PostgresRepository) Create(ctx context.Context, req *domain.Request) error {
	_, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		INSERT INTO customer_data_requests (id, garage_id, customer_id, type, status, source, details, outcome, requested_by,
		                                    requested_at, completed_at, completed_by)
		VALUES ($1, $2, $3, $4::data_request_type, $5::data_request_status, $6::data_request_source, $7, $8, $9, $10, $11, $12)`,
		req.ID, req.GarageID, req.CustomerID, string(req.Type), string(req.Status), string(req.Source), req.Details, req.Outcome,
		req.RequestedBy, req.RequestedAt, req.CompletedAt, req.CompletedBy)

	return err
}

func (r *PostgresRepository) find(ctx context.Context, garageID, requestID, lock string) (*domain.Request, error) {
	if !isUUID(requestID) {
		return nil, domain.ErrRequestNotFound
	}

	return scanRequest(platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT `+requestColumns+` FROM customer_data_requests WHERE garage_id = $1 AND id = $2`+lock, garageID, requestID))
}

// Find returns a request, or domain.ErrRequestNotFound.
func (r *PostgresRepository) Find(ctx context.Context, garageID, requestID string) (*domain.Request, error) {
	return r.find(ctx, garageID, requestID, "")
}

// FindForUpdate returns a request locked until the transaction ends.
func (r *PostgresRepository) FindForUpdate(ctx context.Context, garageID, requestID string) (*domain.Request, error) {
	return r.find(ctx, garageID, requestID, " FOR UPDATE")
}

// Save persists the status, outcome and completion of a request.
func (r *PostgresRepository) Save(ctx context.Context, req *domain.Request) error {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		UPDATE customer_data_requests SET status = $3::data_request_status, outcome = $4, completed_at = $5, completed_by = $6
		WHERE garage_id = $1 AND id = $2`, req.GarageID, req.ID, string(req.Status), req.Outcome, req.CompletedAt, req.CompletedBy)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrRequestNotFound
	}

	return nil
}

// List returns one page of a garage's requests, newest first, and the total.
func (r *PostgresRepository) List(ctx context.Context, garageID string, f usecase.Filter) ([]*domain.Request, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := ` FROM customer_data_requests WHERE garage_id = $1
		AND ($2::text = '' OR customer_id::text = $2) AND ($3::text = '' OR type = NULLIF($3, '')::data_request_type)
		AND ($4::text = '' OR status = NULLIF($4, '')::data_request_status)`
	args := []any{garageID, f.CustomerID, string(f.Type), string(f.Status)}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+requestColumns+where+` ORDER BY requested_at DESC, id LIMIT $5 OFFSET $6`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.Request, 0, f.Limit)

	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			return nil, 0, err
		}

		items = append(items, req)
	}

	return items, total, rows.Err()
}

// ClearDetails empties the details of every request of the customer and the
// outcome of its corrections, which may repeat what was corrected, and closes
// the corrections still waiting: there is nothing left to correct.
func (r *PostgresRepository) ClearDetails(ctx context.Context, garageID, customerID string) error {
	_, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx,
		`UPDATE customer_data_requests SET details = '',
			outcome = CASE WHEN type = 'correct' AND (outcome <> '' OR status = 'pending') THEN 'Cleared when the customer was anonymized' ELSE outcome END,
			status = CASE WHEN status = 'pending' THEN 'rejected'::data_request_status ELSE status END,
			completed_at = CASE WHEN status = 'pending' THEN now() ELSE completed_at END
		WHERE garage_id = $1 AND customer_id = $2 AND (details <> '' OR status = 'pending' OR (type = 'correct' AND outcome <> ''))`, garageID, customerID)

	return err
}

// ---- Requests that wait for their link ------------------------------------

// CreateConfirmation inserts a waiting request.
func (r *PostgresRepository) CreateConfirmation(ctx context.Context, c *domain.Confirmation) error {
	_, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		INSERT INTO data_request_confirmations (id, email, type, details, locale, token_hash, source_hash, expires_at, created_at)
		VALUES ($1, $2, $3::data_request_type, $4, $5, $6, $7, $8, $9)`,
		c.ID, c.Email, string(c.Type), c.Details, c.Locale, c.TokenHash, c.SourceHash, c.ExpiresAt, c.CreatedAt)

	return err
}

// CountByEmail counts the requests of an address created at or after since.
func (r *PostgresRepository) CountByEmail(ctx context.Context, address string, since time.Time) (int, error) {
	var n int

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT count(*) FROM data_request_confirmations WHERE lower(email) = lower($1) AND created_at >= $2`, address, since).Scan(&n)

	return n, err
}

// CountBySource counts the requests of a source created at or after since.
func (r *PostgresRepository) CountBySource(ctx context.Context, sourceHash string, since time.Time) (int, error) {
	var n int

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx,
		`SELECT count(*) FROM data_request_confirmations WHERE source_hash = $1 AND created_at >= $2`, sourceHash, since).Scan(&n)

	return n, err
}

// Consume deletes and returns the unexpired waiting request with this token
// hash in one statement, so a link works once, or domain.ErrTokenInvalid.
func (r *PostgresRepository) Consume(ctx context.Context, tokenHash string, now time.Time) (*domain.Confirmation, error) {
	var (
		c   domain.Confirmation
		typ string
	)

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		DELETE FROM data_request_confirmations WHERE token_hash = $1 AND expires_at > $2
		RETURNING id::text, email, type::text, details, locale, token_hash, source_hash, expires_at, created_at`, tokenHash, now,
	).Scan(&c.ID, &c.Email, &typ, &c.Details, &c.Locale, &c.TokenHash, &c.SourceHash, &c.ExpiresAt, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrTokenInvalid
		}

		return nil, err
	}

	c.Type = domain.Type(typ)

	return &c, nil
}

// PurgeExpired deletes the waiting requests whose link expired before now.
func (r *PostgresRepository) PurgeExpired(ctx context.Context, now time.Time) (int, error) {
	tag, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `DELETE FROM data_request_confirmations WHERE expires_at < $1`, now)
	if err != nil {
		return 0, err
	}

	return int(tag.RowsAffected()), nil
}

// Numbers lists the numbers of the documents a customer has: quotes, repair
// orders (through the customer's vehicles), settlements and payment vouchers.
// It only reads, to answer a view request (FR-CUS-05).
func (r *PostgresRepository) Numbers(ctx context.Context, garageID, customerID string) (usecase.DocumentNumbers, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	out := usecase.DocumentNumbers{Quotes: []string{}, RepairOrders: []string{}, Settlements: []string{}, PaymentVouchers: []string{}}

	for _, c := range []struct {
		into  *[]string
		query string
	}{
		{&out.Quotes, `SELECT number FROM quotes WHERE garage_id = $1 AND customer_id = $2 AND deleted_at IS NULL ORDER BY created_at, number`},
		{&out.RepairOrders, `SELECT ro.number FROM repair_orders ro JOIN vehicles v ON v.garage_id = ro.garage_id AND v.id = ro.vehicle_id
			WHERE ro.garage_id = $1 AND v.customer_id = $2 AND ro.deleted_at IS NULL ORDER BY ro.created_at, ro.number`},
		{&out.Settlements, `SELECT s.number FROM settlements s JOIN repair_orders ro ON ro.garage_id = s.garage_id AND ro.id = s.repair_order_id
			JOIN vehicles v ON v.garage_id = ro.garage_id AND v.id = ro.vehicle_id
			WHERE s.garage_id = $1 AND v.customer_id = $2 ORDER BY s.created_at, s.number`},
		{&out.PaymentVouchers, `SELECT p.voucher_number FROM settlement_payments p JOIN settlements s ON s.garage_id = p.garage_id AND s.id = p.settlement_id
			JOIN repair_orders ro ON ro.garage_id = s.garage_id AND ro.id = s.repair_order_id
			JOIN vehicles v ON v.garage_id = ro.garage_id AND v.id = ro.vehicle_id
			WHERE p.garage_id = $1 AND v.customer_id = $2 ORDER BY p.created_at, p.voucher_number`},
	} {
		rows, err := q.Query(ctx, c.query, garageID, customerID)
		if err != nil {
			return out, err
		}

		for rows.Next() {
			var number string
			if err := rows.Scan(&number); err != nil {
				rows.Close()

				return out, err
			}

			*c.into = append(*c.into, number)
		}

		rows.Close()

		if err := rows.Err(); err != nil {
			return out, err
		}
	}

	return out, nil
}
