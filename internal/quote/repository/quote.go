package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
)

// Create inserts q and its lines.
func (r *PostgresRepository) Create(ctx context.Context, q *domain.Quote) error {
	q0 := platformdb.QuerierFromContext(ctx, r.pool)

	err := q0.QueryRow(ctx, `
		INSERT INTO quotes (id, garage_id, customer_id, vehicle_id, customer_name, vehicle_license_plate, number, status,
			approval_mode, notes, total_amount, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at`,
		q.ID, q.GarageID, q.CustomerID, q.VehicleID, q.CustomerName, q.VehicleLicensePlate, q.Number, string(q.Status),
		string(q.ApprovalMode), q.Notes, q.TotalAmount, q.CreatedBy, q.UpdatedBy,
	).Scan(&q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		return err
	}

	return r.insertLines(ctx, q0, q.GarageID, q.ID, q.Lines)
}

func (r *PostgresRepository) insertLines(ctx context.Context, q platformdb.Querier, garageID, quoteID string, lines []domain.Line) error {
	for _, line := range lines {
		_, err := q.Exec(ctx, `
			INSERT INTO quote_lines (id, garage_id, quote_id, position, item_type, item_ref_id, description, quantity,
				unit_price, line_total, decision)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			line.ID, garageID, quoteID, line.Position, string(line.ItemType), line.ItemRefID, line.Description,
			line.Quantity, line.UnitPrice, line.LineTotal, string(line.Decision),
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// FindByID returns the live quote with its lines, or domain.ErrQuoteNotFound.
func (r *PostgresRepository) FindByID(ctx context.Context, garageID, quoteID string) (*domain.Quote, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	quote, err := scanQuote(q.QueryRow(ctx, `
		SELECT `+quoteColumns+` FROM quotes WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`,
		garageID, quoteID))
	if err != nil {
		return nil, err
	}

	lines, err := r.linesFor(ctx, q, garageID, quoteID)
	if err != nil {
		return nil, err
	}

	quote.Lines = lines

	return quote, nil
}

func (r *PostgresRepository) linesFor(ctx context.Context, q platformdb.Querier, garageID, quoteID string) ([]domain.Line, error) {
	rows, err := q.Query(ctx, `
		SELECT `+lineColumns+` FROM quote_lines WHERE garage_id = $1 AND quote_id = $2 ORDER BY position`,
		garageID, quoteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []domain.Line

	for rows.Next() {
		line, err := scanLine(rows)
		if err != nil {
			return nil, err
		}

		lines = append(lines, line)
	}

	return lines, rows.Err()
}

// ReplaceLines persists q's scalar fields and replaces its whole line set;
// only called while the quote is a draft.
func (r *PostgresRepository) ReplaceLines(ctx context.Context, q *domain.Quote) error {
	if err := r.saveScalars(ctx, q); err != nil {
		return err
	}

	querier := platformdb.QuerierFromContext(ctx, r.pool)
	if _, err := querier.Exec(ctx, `DELETE FROM quote_lines WHERE garage_id = $1 AND quote_id = $2`, q.GarageID, q.ID); err != nil {
		return err
	}

	return r.insertLines(ctx, querier, q.GarageID, q.ID, q.Lines)
}

// Save persists q's scalar fields and each line's decision, without
// touching line content.
func (r *PostgresRepository) Save(ctx context.Context, q *domain.Quote) error {
	if err := r.saveScalars(ctx, q); err != nil {
		return err
	}

	querier := platformdb.QuerierFromContext(ctx, r.pool)

	for _, line := range q.Lines {
		_, err := querier.Exec(ctx, `
			UPDATE quote_lines SET decision = $3, decided_at = $4
			WHERE garage_id = $1 AND id = $2`,
			q.GarageID, line.ID, string(line.Decision), line.DecidedAt)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *PostgresRepository) saveScalars(ctx context.Context, q *domain.Quote) error {
	querier := platformdb.QuerierFromContext(ctx, r.pool)

	row := querier.QueryRow(ctx, `
		UPDATE quotes SET customer_id = $3, vehicle_id = $4, customer_name = $5, vehicle_license_plate = $6,
			status = $7, approval_mode = $8, notes = $9, total_amount = $10, public_token_hash = $11,
			public_token_expires_at = $12, sent_at = $13, decided_at = $14, converted_at = $15, updated_by = $16
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING updated_at`,
		q.GarageID, q.ID, q.CustomerID, q.VehicleID, q.CustomerName, q.VehicleLicensePlate, string(q.Status),
		string(q.ApprovalMode), q.Notes, q.TotalAmount, q.PublicTokenHash, q.PublicTokenExpiresAt, q.SentAt,
		q.DecidedAt, q.ConvertedAt, q.UpdatedBy,
	)

	return scanWrite(row, domain.ErrQuoteNotFound, &q.UpdatedAt)
}

func scanWrite(row pgx.Row, notFound error, dest ...any) error {
	err := row.Scan(dest...)
	if err != nil {
		return translateNotFound(err, notFound)
	}

	return nil
}

// List returns one page of live quotes (without lines), newest first.
func (r *PostgresRepository) List(ctx context.Context, garageID string, filter usecase.ListFilter) ([]*domain.Quote, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := `WHERE garage_id = $1 AND deleted_at IS NULL
		AND ($2::uuid IS NULL OR customer_id = $2::uuid)
		AND ($3::uuid IS NULL OR vehicle_id = $3::uuid)
		AND ($4::quote_status IS NULL OR status = $4::quote_status)`
	args := []any{garageID, nullableUUID(filter.CustomerID), nullableUUID(filter.VehicleID), nullableString(string(filter.Status))}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM quotes `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+quoteColumns+` FROM quotes `+where+`
		ORDER BY created_at DESC, id LIMIT $5 OFFSET $6`,
		append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.Quote, 0, filter.Limit)

	for rows.Next() {
		item, err := scanQuote(rows)
		if err != nil {
			return nil, 0, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// SoftDelete marks a draft quote deleted, or returns domain.ErrQuoteNotFound
// (also when it exists but is no longer a draft, since it is then hidden
// from further edits by RLS-visible status alone -- the use case checks
// editability before calling this).
func (r *PostgresRepository) SoftDelete(ctx context.Context, garageID, quoteID, deletedBy string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	tag, err := q.Exec(ctx, `
		UPDATE quotes SET deleted_at = now(), updated_by = $3
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`,
		garageID, quoteID, nullableString(deletedBy))
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrQuoteNotFound
	}

	return nil
}

// FindByPublicToken resolves an approval-link token via the SECURITY
// DEFINER function find_quote_by_public_token (migration 000015), which
// bypasses the caller's (nonexistent) garage scope.
func (r *PostgresRepository) FindByPublicToken(ctx context.Context, tokenHash string) (string, string, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	var quoteID, garageID string

	err := q.QueryRow(ctx, `SELECT id, garage_id FROM find_quote_by_public_token($1)`, tokenHash).Scan(&quoteID, &garageID)
	if err != nil {
		return "", "", translateNotFound(err, domain.ErrPublicTokenInvalid)
	}

	return garageID, quoteID, nil
}

func translateNotFound(err, notFound error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound
	}

	return err
}

func nullableUUID(id string) *string {
	if id == "" {
		return nil
	}

	return &id
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
