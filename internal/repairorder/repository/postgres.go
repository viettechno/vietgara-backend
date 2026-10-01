// Package repository implements the persistence layer of the Repair Order
// module on top of the shared Postgres pool. Every query filters on
// garage_id explicitly on top of Row-Level Security and skips soft-deleted
// rows. See: Database Design Section 4.3-4.4.
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

const pgUniqueViolation = "23505"

// PostgresRepository implements usecase.Repository over the shared pgx
// pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns a repair order repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create inserts ro and its lines. A quote already converted (the unique
// constraint on quote_id) returns domain.ErrAlreadyConverted.
func (r *PostgresRepository) Create(ctx context.Context, ro *domain.RepairOrder) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO repair_orders (id, garage_id, quote_id, vehicle_id, number, status, total_amount, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at, updated_at`,
		ro.ID, ro.GarageID, ro.QuoteID, ro.VehicleID, ro.Number, string(ro.Status), ro.TotalAmount, ro.CreatedBy, ro.UpdatedBy,
	).Scan(&ro.CreatedAt, &ro.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "repair_orders_quote_id_unique" {
			return domain.ErrAlreadyConverted
		}

		return err
	}

	for _, line := range ro.Lines {
		_, err := q.Exec(ctx, `
			INSERT INTO repair_order_lines
				(id, garage_id, repair_order_id, position, item_type, item_ref_id, description, quantity, unit_price, line_total)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			line.ID, ro.GarageID, ro.ID, line.Position, string(line.ItemType), line.ItemRefID, line.Description,
			line.Quantity, line.UnitPrice, line.LineTotal,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// ListForVehicle returns a vehicle's live repair orders, newest first,
// without their lines (FR-CUS-03 only needs the summary).
func (r *PostgresRepository) ListForVehicle(ctx context.Context, garageID, vehicleID string) ([]*domain.RepairOrder, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT id, garage_id, quote_id, vehicle_id, number, status, total_amount, closed_at, created_at, updated_at
		FROM repair_orders
		WHERE garage_id = $1 AND vehicle_id = $2 AND deleted_at IS NULL
		ORDER BY created_at DESC`,
		garageID, vehicleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.RepairOrder

	for rows.Next() {
		ro, err := scanRepairOrder(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, ro)
	}

	return out, rows.Err()
}

func scanRepairOrder(row pgx.Row) (*domain.RepairOrder, error) {
	var (
		ro     domain.RepairOrder
		status string
	)

	err := row.Scan(&ro.ID, &ro.GarageID, &ro.QuoteID, &ro.VehicleID, &ro.Number, &status, &ro.TotalAmount,
		&ro.ClosedAt, &ro.CreatedAt, &ro.UpdatedAt)
	if err != nil {
		return nil, err
	}

	ro.Status = domain.RepairOrderStatus(status)

	return &ro, nil
}
