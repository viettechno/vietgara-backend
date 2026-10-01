// Package repository implements the persistence layer of the Quote &
// Pricing module on top of the shared Postgres pool. Every query filters
// on garage_id explicitly on top of Row-Level Security and skips
// soft-deleted rows; it never queries another module's tables (customer
// name and vehicle plate are denormalized on the quote itself, see
// migration 000015).
// See: Database Design Section 4.1-4.2, LLD Section 2.
package repository

import (
	"errors"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/quote/domain"
)

// PostgresRepository implements usecase.Repository over the shared pgx
// pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a quote repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const quoteColumns = `id, garage_id, customer_id, vehicle_id, customer_name, vehicle_license_plate, number, status,
	approval_mode, notes, total_amount, public_token_hash, public_token_expires_at, sent_at, decided_at, converted_at,
	created_by, updated_by, created_at, updated_at`

func scanQuote(row pgx.Row) (*domain.Quote, error) {
	var (
		q      domain.Quote
		status string
		mode   string
	)

	err := row.Scan(&q.ID, &q.GarageID, &q.CustomerID, &q.VehicleID, &q.CustomerName, &q.VehicleLicensePlate, &q.Number,
		&status, &mode, &q.Notes, &q.TotalAmount, &q.PublicTokenHash, &q.PublicTokenExpiresAt, &q.SentAt, &q.DecidedAt,
		&q.ConvertedAt, &q.CreatedBy, &q.UpdatedBy, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrQuoteNotFound
		}

		return nil, err
	}

	q.Status, q.ApprovalMode = domain.Status(status), domain.ApprovalMode(mode)

	return &q, nil
}

const lineColumns = `id, position, item_type, item_ref_id, description, quantity, unit_price, line_total, decision, decided_at`

func scanLine(row pgx.Row) (domain.Line, error) {
	var (
		line     domain.Line
		itemType string
		decision string
	)

	err := row.Scan(&line.ID, &line.Position, &itemType, &line.ItemRefID, &line.Description, &line.Quantity,
		&line.UnitPrice, &line.LineTotal, &decision, &line.DecidedAt)
	if err != nil {
		return domain.Line{}, err
	}

	line.ItemType, line.Decision = domain.ItemType(itemType), domain.LineDecision(decision)

	return line, nil
}
