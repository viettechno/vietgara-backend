// Package repository implements the persistence layer of the Repair Order
// module on top of the shared Postgres pool. Every query filters on
// garage_id explicitly on top of Row-Level Security and skips soft-deleted
// rows; it never queries another module's tables (customer name and
// vehicle plate are denormalized on the repair order itself, see
// migration 000016). See: Database Design Section 4.3-4.4.
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
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

const repairOrderColumns = `id, garage_id, quote_id, vehicle_id, vehicle_license_plate, customer_name, number, status,
	total_amount, closed_at, created_by, updated_by, created_at, updated_at`

// repairOrderListColumns is repairOrderColumns qualified by the "ro" alias
// List's query joins repair_order_lines under.
const repairOrderListColumns = `ro.id, ro.garage_id, ro.quote_id, ro.vehicle_id, ro.vehicle_license_plate, ro.customer_name,
	ro.number, ro.status, ro.total_amount, ro.closed_at, ro.created_by, ro.updated_by, ro.created_at, ro.updated_at`

func scanRepairOrder(row pgx.Row) (*domain.RepairOrder, error) {
	var (
		ro     domain.RepairOrder
		status string
	)

	err := row.Scan(&ro.ID, &ro.GarageID, &ro.QuoteID, &ro.VehicleID, &ro.VehicleLicensePlate, &ro.CustomerName, &ro.Number,
		&status, &ro.TotalAmount, &ro.ClosedAt, &ro.CreatedBy, &ro.UpdatedBy, &ro.CreatedAt, &ro.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRepairOrderNotFound
		}

		return nil, err
	}

	ro.Status = domain.RepairOrderStatus(status)

	return &ro, nil
}

const lineColumns = `id, position, item_type, item_ref_id, description, quantity, unit_price, line_total, status, assigned_technicians`

func scanLine(row pgx.Row) (domain.Line, error) {
	var (
		line     domain.Line
		itemType string
		status   string
	)

	err := row.Scan(&line.ID, &line.Position, &itemType, &line.ItemRefID, &line.Description, &line.Quantity,
		&line.UnitPrice, &line.LineTotal, &status, &line.AssignedTechnicianIDs)
	if err != nil {
		return domain.Line{}, err
	}

	line.ItemType, line.Status = domain.ItemType(itemType), domain.LineStatus(status)

	return line, nil
}

// Create inserts ro and its lines. A quote already converted returns
// domain.ErrAlreadyConverted.
func (r *PostgresRepository) Create(ctx context.Context, ro *domain.RepairOrder) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	row := q.QueryRow(ctx, `
		INSERT INTO repair_orders (id, garage_id, quote_id, vehicle_id, vehicle_license_plate, customer_name, number,
			status, total_amount, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at, updated_at`,
		ro.ID, ro.GarageID, ro.QuoteID, ro.VehicleID, ro.VehicleLicensePlate, ro.CustomerName, ro.Number,
		string(ro.Status), ro.TotalAmount, ro.CreatedBy, ro.UpdatedBy,
	)

	err := row.Scan(&ro.CreatedAt, &ro.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "repair_orders_quote_id_unique") {
			return domain.ErrAlreadyConverted
		}

		return err
	}

	return r.insertLines(ctx, q, ro.GarageID, ro.ID, ro.Lines)
}

func (r *PostgresRepository) insertLines(ctx context.Context, q platformdb.Querier, garageID, repairOrderID string, lines []domain.Line) error {
	for _, line := range lines {
		_, err := q.Exec(ctx, `
			INSERT INTO repair_order_lines
				(id, garage_id, repair_order_id, position, item_type, item_ref_id, description, quantity, unit_price,
				 line_total, status, assigned_technicians)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			line.ID, garageID, repairOrderID, line.Position, string(line.ItemType), line.ItemRefID, line.Description,
			line.Quantity, line.UnitPrice, line.LineTotal, string(line.Status), technicianIDs(line.AssignedTechnicianIDs),
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// FindByID returns the live repair order with its lines, or
// domain.ErrRepairOrderNotFound.
func (r *PostgresRepository) FindByID(ctx context.Context, garageID, repairOrderID string) (*domain.RepairOrder, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	ro, err := scanRepairOrder(q.QueryRow(ctx, `
		SELECT `+repairOrderColumns+` FROM repair_orders WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`,
		garageID, repairOrderID))
	if err != nil {
		return nil, err
	}

	lines, err := r.linesFor(ctx, q, garageID, repairOrderID)
	if err != nil {
		return nil, err
	}

	ro.Lines = lines

	return ro, nil
}

func (r *PostgresRepository) linesFor(ctx context.Context, q platformdb.Querier, garageID, repairOrderID string) ([]domain.Line, error) {
	rows, err := q.Query(ctx, `
		SELECT `+lineColumns+` FROM repair_order_lines WHERE garage_id = $1 AND repair_order_id = $2 ORDER BY position`,
		garageID, repairOrderID)
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

// Save persists ro's scalar fields and each line's status and assigned
// technicians, without touching line content.
func (r *PostgresRepository) Save(ctx context.Context, ro *domain.RepairOrder) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	row := q.QueryRow(ctx, `
		UPDATE repair_orders SET status = $3, total_amount = $4, closed_at = $5, updated_by = $6
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING updated_at`,
		ro.GarageID, ro.ID, string(ro.Status), ro.TotalAmount, ro.ClosedAt, ro.UpdatedBy,
	)

	err := row.Scan(&ro.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrRepairOrderNotFound
		}

		return err
	}

	for _, line := range ro.Lines {
		_, err := q.Exec(ctx, `
			UPDATE repair_order_lines SET status = $3, assigned_technicians = $4
			WHERE garage_id = $1 AND id = $2`,
			ro.GarageID, line.ID, string(line.Status), technicianIDs(line.AssignedTechnicianIDs))
		if err != nil {
			return err
		}
	}

	return nil
}

// List returns one page of live repair orders (without lines), newest
// first.
func (r *PostgresRepository) List(ctx context.Context, garageID string, filter usecase.ListFilter) ([]*domain.RepairOrder, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := `WHERE ro.garage_id = $1 AND ro.deleted_at IS NULL
		AND ($2::uuid IS NULL OR ro.vehicle_id = $2::uuid)
		AND ($3::repair_order_status IS NULL OR ro.status = $3::repair_order_status)
		AND ($4::uuid IS NULL OR EXISTS (
			SELECT 1 FROM repair_order_lines l WHERE l.repair_order_id = ro.id AND l.assigned_technicians @> ARRAY[$4::uuid]
		))`
	args := []any{garageID, nullableUUID(filter.VehicleID), nullableString(string(filter.Status)), nullableUUID(filter.TechnicianID)}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM repair_orders ro `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+repairOrderListColumns+` FROM repair_orders ro `+where+`
		ORDER BY ro.created_at DESC, ro.id LIMIT $5 OFFSET $6`,
		append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.RepairOrder, 0, filter.Limit)

	for rows.Next() {
		item, err := scanRepairOrder(rows)
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

// ListForVehicle returns a vehicle's live repair orders, newest first,
// without their lines (FR-CUS-03 only needs the summary).
func (r *PostgresRepository) ListForVehicle(ctx context.Context, garageID, vehicleID string) ([]*domain.RepairOrder, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT `+repairOrderColumns+` FROM repair_orders
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

func technicianIDs(ids []string) []string {
	if ids == nil {
		return []string{}
	}

	return ids
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

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == constraint
}
