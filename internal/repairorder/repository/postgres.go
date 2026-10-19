// Package repository implements the persistence layer of the Repair Order
// module on top of the shared Postgres pool. Every query filters on
// garage_id explicitly on top of Row-Level Security and skips soft-deleted
// rows; it never queries another module's tables (customer name and
// vehicle plate are denormalized on the repair order itself, see
// migration 000016). See: Database Design Section 4.3-4.4.
package repository

import (
	"context"
	"encoding/json"
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
	total_amount, issuer, billing_party, closed_at, waiting_on_parts, created_by, updated_by, created_at, updated_at`

// repairOrderListColumns is repairOrderColumns qualified by the "ro" alias
// List's query joins repair_order_lines under.
const repairOrderListColumns = `ro.id, ro.garage_id, ro.quote_id, ro.vehicle_id, ro.vehicle_license_plate, ro.customer_name,
	ro.number, ro.status, ro.total_amount, ro.issuer, ro.billing_party, ro.closed_at, ro.waiting_on_parts, ro.created_by, ro.updated_by,
	ro.created_at, ro.updated_at`

func scanRepairOrder(row pgx.Row) (*domain.RepairOrder, error) {
	var (
		ro     domain.RepairOrder
		status string
		issuer []byte
		party  []byte
	)

	err := row.Scan(&ro.ID, &ro.GarageID, &ro.QuoteID, &ro.VehicleID, &ro.VehicleLicensePlate, &ro.CustomerName, &ro.Number,
		&status, &ro.TotalAmount, &issuer, &party, &ro.ClosedAt, &ro.WaitingOnParts, &ro.CreatedBy, &ro.UpdatedBy, &ro.CreatedAt, &ro.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrRepairOrderNotFound
		}

		return nil, err
	}

	ro.Status = domain.RepairOrderStatus(status)

	if len(issuer) > 0 {
		if err := json.Unmarshal(issuer, &ro.Issuer); err != nil {
			return nil, err
		}
	}

	if len(party) > 0 {
		if err := json.Unmarshal(party, &ro.BillingParty); err != nil {
			return nil, err
		}
	}

	return &ro, nil
}

// lineColumns reads the packaging unit's abbreviation from the Catalogue's
// units table (a foreign key points there already).
const lineColumns = `id, position, item_type, item_ref_id, description, trim_scale(quantity)::text, unit_id,
	trim_scale(entered_quantity)::text,
	COALESCE((SELECT u.abbreviation FROM units u WHERE u.garage_id = repair_order_lines.garage_id AND u.id = repair_order_lines.unit_id), ''),
	unit_price, line_total, status, assigned_technicians, job_id, non_billable`

func scanLine(row pgx.Row) (domain.Line, error) {
	var (
		line     domain.Line
		itemType string
		status   string
	)

	err := row.Scan(&line.ID, &line.Position, &itemType, &line.ItemRefID, &line.Description, &line.Quantity,
		&line.UnitID, &line.EnteredQuantity, &line.UnitAbbreviation, &line.UnitPrice, &line.LineTotal, &status, &line.AssignedTechnicianIDs,
		&line.JobID, &line.NonBillable)
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

	issuer, err := json.Marshal(ro.Issuer)
	if err != nil {
		return err
	}

	party, err := json.Marshal(ro.BillingParty)
	if err != nil {
		return err
	}

	row := q.QueryRow(ctx, `
		INSERT INTO repair_orders (id, garage_id, quote_id, vehicle_id, vehicle_license_plate, customer_name, number,
			status, total_amount, issuer, billing_party, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at`,
		ro.ID, ro.GarageID, ro.QuoteID, ro.VehicleID, ro.VehicleLicensePlate, ro.CustomerName, ro.Number,
		string(ro.Status), ro.TotalAmount, issuer, party, ro.CreatedBy, ro.UpdatedBy,
	)

	err = row.Scan(&ro.CreatedAt, &ro.UpdatedAt)
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
				(id, garage_id, repair_order_id, position, item_type, item_ref_id, description, quantity, unit_id,
				 entered_quantity, unit_price, line_total, status, assigned_technicians, job_id, non_billable)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9, $10::numeric, $11, $12, $13, $14, $15, $16)`,
			line.ID, garageID, repairOrderID, line.Position, string(line.ItemType), line.ItemRefID, line.Description,
			line.Quantity, line.UnitID, line.EnteredQuantity, line.UnitPrice, line.LineTotal, string(line.Status), technicianIDs(line.AssignedTechnicianIDs),
			line.JobID, line.NonBillable,
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
		UPDATE repair_orders SET status = $3, total_amount = $4, closed_at = $5, waiting_on_parts = $7, updated_by = $6
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING updated_at`,
		ro.GarageID, ro.ID, string(ro.Status), ro.TotalAmount, ro.ClosedAt, ro.UpdatedBy, ro.WaitingOnParts,
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

// ReplaceLines persists ro like Save and also the content of its lines: lines
// no longer in ro.Lines are deleted, the others are inserted or updated in
// place (their ids stay, which the stock issue refers to).
func (r *PostgresRepository) ReplaceLines(ctx context.Context, ro *domain.RepairOrder) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	kept := make([]string, 0, len(ro.Lines))
	for _, line := range ro.Lines {
		kept = append(kept, line.ID)
	}

	if _, err := q.Exec(ctx,
		`DELETE FROM repair_order_lines WHERE garage_id = $1 AND repair_order_id = $2 AND NOT (id = ANY($3::uuid[]))`,
		ro.GarageID, ro.ID, kept); err != nil {
		return err
	}

	for _, line := range ro.Lines {
		_, err := q.Exec(ctx, `
			INSERT INTO repair_order_lines
				(id, garage_id, repair_order_id, position, item_type, item_ref_id, description, quantity, unit_id,
				 entered_quantity, unit_price, line_total, status, assigned_technicians, job_id, non_billable)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $9, $10::numeric, $11, $12, $13, $14, $15, $16)
			ON CONFLICT (id) DO UPDATE SET position = EXCLUDED.position, item_type = EXCLUDED.item_type,
				item_ref_id = EXCLUDED.item_ref_id, description = EXCLUDED.description, quantity = EXCLUDED.quantity,
				unit_id = EXCLUDED.unit_id, entered_quantity = EXCLUDED.entered_quantity, unit_price = EXCLUDED.unit_price,
				line_total = EXCLUDED.line_total, status = EXCLUDED.status, assigned_technicians = EXCLUDED.assigned_technicians,
				job_id = EXCLUDED.job_id, non_billable = EXCLUDED.non_billable`,
			line.ID, ro.GarageID, ro.ID, line.Position, string(line.ItemType), line.ItemRefID, line.Description,
			line.Quantity, line.UnitID, line.EnteredQuantity, line.UnitPrice, line.LineTotal, string(line.Status),
			technicianIDs(line.AssignedTechnicianIDs), line.JobID, line.NonBillable)
		if err != nil {
			return err
		}
	}

	return r.Save(ctx, ro)
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

// ExistsForVehicles reports whether any live repair order belongs to one of
// the vehicles.
func (r *PostgresRepository) ExistsForVehicles(ctx context.Context, garageID string, vehicleIDs []string) (bool, error) {
	if len(vehicleIDs) == 0 {
		return false, nil
	}

	var exists bool

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM repair_orders
			WHERE garage_id = $1 AND vehicle_id = ANY($2::uuid[]) AND deleted_at IS NULL)`,
		garageID, vehicleIDs).Scan(&exists)

	return exists, err
}

// LiveVehicleIDs returns which of the vehicles a live repair order belongs
// to, in one query (FR-CUS-02).
func (r *PostgresRepository) LiveVehicleIDs(ctx context.Context, garageID string, vehicleIDs []string) ([]string, error) {
	if len(vehicleIDs) == 0 {
		return nil, nil
	}

	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT DISTINCT vehicle_id FROM repair_orders
		WHERE garage_id = $1 AND deleted_at IS NULL AND vehicle_id = ANY($2::uuid[])`, garageID, vehicleIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
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

// LinesReference reports whether a line refers to the part (as its item_ref_id)
// or uses the unit, in a live or a deleted document: a unit cannot be deleted
// while any line keeps its foreign key, and a part that documents mention is
// deactivated, not deleted (FR-CAT-07). partID or unitID may be "" to skip it.
func (r *PostgresRepository) LinesReference(ctx context.Context, garageID, partID, unitID string) (bool, error) {
	var exists bool

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM repair_order_lines
			WHERE garage_id = $1 AND (($2::text <> '' AND item_ref_id = NULLIF($2, '')::uuid)
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))`,
		garageID, partID, unitID).Scan(&exists)

	return exists, err
}
