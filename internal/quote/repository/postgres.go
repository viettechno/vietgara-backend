// Package repository implements the persistence layer of the Quote &
// Pricing module on top of the shared Postgres pool. Every query filters
// on garage_id explicitly on top of Row-Level Security and skips
// soft-deleted rows; it never queries another module's tables (customer
// name and vehicle plate are denormalized on the quote itself, see
// migration 000015).
// See: Database Design Section 4.1-4.2, LLD Section 2.
package repository

import (
	"encoding/json"
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
	approval_mode, notes, total_amount, issuer, billing_party, public_token_hash, public_token_expires_at, sent_at, decided_at, converted_at,
	created_by, updated_by, created_at, updated_at`

func scanQuote(row pgx.Row) (*domain.Quote, error) {
	var (
		q      domain.Quote
		status string
		mode   string
		issuer []byte
		party  []byte
	)

	err := row.Scan(&q.ID, &q.GarageID, &q.CustomerID, &q.VehicleID, &q.CustomerName, &q.VehicleLicensePlate, &q.Number,
		&status, &mode, &q.Notes, &q.TotalAmount, &issuer, &party, &q.PublicTokenHash, &q.PublicTokenExpiresAt, &q.SentAt, &q.DecidedAt,
		&q.ConvertedAt, &q.CreatedBy, &q.UpdatedBy, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrQuoteNotFound
		}

		return nil, err
	}

	q.Status, q.ApprovalMode = domain.Status(status), domain.ApprovalMode(mode)

	if len(issuer) > 0 {
		if err := json.Unmarshal(issuer, &q.Issuer); err != nil {
			return nil, err
		}
	}

	if len(party) > 0 {
		if err := json.Unmarshal(party, &q.BillingParty); err != nil {
			return nil, err
		}
	}

	return &q, nil
}

// lineColumns reads the packaging unit's abbreviation from the Catalogue's
// units table (a foreign key points there already).
const lineColumns = `id, position, item_type, item_ref_id, description, trim_scale(quantity)::text, unit_id,
	trim_scale(entered_quantity)::text,
	COALESCE((SELECT u.abbreviation FROM units u WHERE u.garage_id = quote_lines.garage_id AND u.id = quote_lines.unit_id), ''),
	unit_price, line_total, decision, decided_at`

func scanLine(row pgx.Row) (domain.Line, error) {
	var (
		line     domain.Line
		itemType string
		decision string
	)

	err := row.Scan(&line.ID, &line.Position, &itemType, &line.ItemRefID, &line.Description, &line.Quantity,
		&line.UnitID, &line.EnteredQuantity, &line.UnitAbbreviation, &line.UnitPrice, &line.LineTotal, &decision, &line.DecidedAt)
	if err != nil {
		return domain.Line{}, err
	}

	line.ItemType, line.Decision = domain.ItemType(itemType), domain.LineDecision(decision)

	return line, nil
}
