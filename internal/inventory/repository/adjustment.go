package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// CreateWriteOff inserts the write-off and its lines.
func (r *PostgresRepository) CreateWriteOff(ctx context.Context, w *domain.WriteOff) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO write_offs (id, garage_id, number, status, reason, notes, created_by, updated_by)
		VALUES ($1, $2, $3, $4::adjustment_status, $5::write_off_reason, $6, $7, $8)
		RETURNING created_at, updated_at`,
		w.ID, w.GarageID, w.Number, string(w.Status), string(w.Reason), w.Notes, w.CreatedBy, w.UpdatedBy,
	).Scan(&w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return err
	}

	for _, l := range w.Lines {
		_, err := q.Exec(ctx, `
			INSERT INTO write_off_lines (id, garage_id, write_off_id, position, part_id, unit_id, entered_quantity, quantity,
			                             unit_cost, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10, $10)`,
			l.ID, w.GarageID, w.ID, l.Position, l.PartID, l.UnitID, l.EnteredQuantity, l.Quantity, l.AverageCost, w.CreatedBy)
		if err != nil {
			return err
		}
	}

	return nil
}

const writeOffColumns = `w.id, w.garage_id, w.number, w.status::text, w.reason::text, w.notes, w.cancelled_at, w.created_by,
	w.updated_by, w.created_at, w.updated_at`

func scanWriteOff(row pgx.Row, extra ...any) (*domain.WriteOff, error) {
	var (
		w              domain.WriteOff
		status, reason string
	)

	dest := append([]any{
		&w.ID, &w.GarageID, &w.Number, &status, &reason, &w.Notes, &w.CancelledAt, &w.CreatedBy, &w.UpdatedBy,
		&w.CreatedAt, &w.UpdatedAt,
	}, extra...)

	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWriteOffNotFound
		}

		return nil, err
	}

	w.Status, w.Reason = domain.AdjustmentStatus(status), domain.WriteOffReason(reason)

	return &w, nil
}

// FindWriteOff returns the write-off with its lines in order.
func (r *PostgresRepository) FindWriteOff(ctx context.Context, garageID, writeOffID string, forUpdate bool) (*domain.WriteOff, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	lock := ""
	if forUpdate {
		lock = forUpdateClause
	}

	w, err := scanWriteOff(q.QueryRow(ctx,
		`SELECT `+writeOffColumns+` FROM write_offs w WHERE w.garage_id = $1 AND w.id = $2`+lock, garageID, writeOffID))
	if err != nil {
		return nil, err
	}

	w.Lines, err = r.adjustmentLines(ctx, `
		SELECT id, position, part_id, unit_id, trim_scale(entered_quantity)::text, trim_scale(quantity)::text,
		       trim_scale(unit_cost)::text, 0
		FROM write_off_lines WHERE garage_id = $1 AND write_off_id = $2 ORDER BY position, id`, garageID, writeOffID)

	return w, err
}

// adjustmentLines runs a query that selects, in order, the id, position, part,
// unit, entered and base quantity, average cost and credit of lines.
func (r *PostgresRepository) adjustmentLines(ctx context.Context, query string, args ...any) ([]domain.AdjustmentLine, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []domain.AdjustmentLine

	for rows.Next() {
		var l domain.AdjustmentLine
		if err := rows.Scan(&l.ID, &l.Position, &l.PartID, &l.UnitID, &l.EnteredQuantity, &l.Quantity, &l.AverageCost,
			&l.CreditUnitCost); err != nil {
			return nil, err
		}

		lines = append(lines, l)
	}

	return lines, rows.Err()
}

// SaveWriteOff persists the status and timestamps of the write-off.
func (r *PostgresRepository) SaveWriteOff(ctx context.Context, w *domain.WriteOff) error {
	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		UPDATE write_offs SET status = $3::adjustment_status, notes = $4, cancelled_at = $5, updated_by = $6
		WHERE garage_id = $1 AND id = $2
		RETURNING updated_at`,
		w.GarageID, w.ID, string(w.Status), w.Notes, w.CancelledAt, w.UpdatedBy,
	).Scan(&w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrWriteOffNotFound
	}

	return err
}

// ListWriteOffs returns one page of write-offs, newest first, with the value of
// their lines.
func (r *PostgresRepository) ListWriteOffs(ctx context.Context, garageID string, filter usecase.WriteOffFilter) ([]*domain.WriteOff, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := ` FROM write_offs w WHERE w.garage_id = $1
		AND ($2::text = '' OR strpos(lower(w.number || ' ' || w.notes), lower($2)) > 0)
		AND ($3::text = '' OR w.reason = NULLIF($3, '')::write_off_reason)
		AND ($4::text = '' OR w.status = NULLIF($4, '')::adjustment_status)`
	args := []any{garageID, filter.Query, string(filter.Reason), string(filter.Status)}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+writeOffColumns+`,
		COALESCE((SELECT sum(round(l.quantity * COALESCE(l.unit_cost, 0))) FROM write_off_lines l
			WHERE l.garage_id = w.garage_id AND l.write_off_id = w.id), 0)::bigint`+where+`
		ORDER BY w.created_at DESC, w.id LIMIT $5 OFFSET $6`, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.WriteOff, 0, filter.Limit)

	for rows.Next() {
		var value int64

		w, err := scanWriteOff(rows, &value)
		if err != nil {
			return nil, 0, err
		}

		w.TotalValue = value
		items = append(items, w)
	}

	return items, total, rows.Err()
}

// CreateSupplierReturn inserts the return and its lines.
func (r *PostgresRepository) CreateSupplierReturn(ctx context.Context, s *domain.SupplierReturn) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO supplier_returns (id, garage_id, number, status, supplier_id, goods_receipt_id, credit_note_number, notes,
		                              created_by, updated_by)
		VALUES ($1, $2, $3, $4::adjustment_status, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at`,
		s.ID, s.GarageID, s.Number, string(s.Status), s.SupplierID, s.GoodsReceiptID, s.CreditNoteNumber, s.Notes, s.CreatedBy,
		s.UpdatedBy,
	).Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return err
	}

	for _, l := range s.Lines {
		_, err := q.Exec(ctx, `
			INSERT INTO supplier_return_lines (id, garage_id, supplier_return_id, position, part_id, unit_id, entered_quantity,
			                                   quantity, unit_cost, average_cost, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9, $10::numeric, $11, $11)`,
			l.ID, s.GarageID, s.ID, l.Position, l.PartID, l.UnitID, l.EnteredQuantity, l.Quantity, l.CreditUnitCost, l.AverageCost,
			s.CreatedBy)
		if err != nil {
			return err
		}
	}

	return nil
}

const supplierReturnColumns = `s.id, s.garage_id, s.number, s.status::text, s.supplier_id, s.goods_receipt_id, s.credit_note_number,
	s.notes, s.cancelled_at, s.created_by, s.updated_by, s.created_at, s.updated_at`

func scanSupplierReturn(row pgx.Row, extra ...any) (*domain.SupplierReturn, error) {
	var (
		s      domain.SupplierReturn
		status string
	)

	dest := append([]any{
		&s.ID, &s.GarageID, &s.Number, &status, &s.SupplierID, &s.GoodsReceiptID, &s.CreditNoteNumber, &s.Notes,
		&s.CancelledAt, &s.CreatedBy, &s.UpdatedBy, &s.CreatedAt, &s.UpdatedAt,
	}, extra...)

	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrSupplierReturnNotFound
		}

		return nil, err
	}

	s.Status = domain.AdjustmentStatus(status)

	return &s, nil
}

// FindSupplierReturn returns the return with its lines in order.
func (r *PostgresRepository) FindSupplierReturn(ctx context.Context, garageID, returnID string, forUpdate bool) (*domain.SupplierReturn, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	lock := ""
	if forUpdate {
		lock = forUpdateClause
	}

	s, err := scanSupplierReturn(q.QueryRow(ctx,
		`SELECT `+supplierReturnColumns+` FROM supplier_returns s WHERE s.garage_id = $1 AND s.id = $2`+lock, garageID, returnID))
	if err != nil {
		return nil, err
	}

	s.Lines, err = r.adjustmentLines(ctx, `
		SELECT id, position, part_id, unit_id, trim_scale(entered_quantity)::text, trim_scale(quantity)::text,
		       trim_scale(average_cost)::text, unit_cost
		FROM supplier_return_lines WHERE garage_id = $1 AND supplier_return_id = $2 ORDER BY position, id`, garageID, returnID)

	return s, err
}

// SaveSupplierReturn persists the status and timestamps of the return.
func (r *PostgresRepository) SaveSupplierReturn(ctx context.Context, s *domain.SupplierReturn) error {
	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		UPDATE supplier_returns SET status = $3::adjustment_status, credit_note_number = $4, notes = $5, cancelled_at = $6,
			updated_by = $7
		WHERE garage_id = $1 AND id = $2
		RETURNING updated_at`,
		s.GarageID, s.ID, string(s.Status), s.CreditNoteNumber, s.Notes, s.CancelledAt, s.UpdatedBy,
	).Scan(&s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrSupplierReturnNotFound
	}

	return err
}

// ListSupplierReturns returns one page of returns, newest first, with their
// credit.
func (r *PostgresRepository) ListSupplierReturns(ctx context.Context, garageID string, filter usecase.SupplierReturnFilter) ([]*domain.SupplierReturn, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := ` FROM supplier_returns s WHERE s.garage_id = $1
		AND ($2::text = '' OR strpos(lower(s.number || ' ' || s.credit_note_number || ' ' || s.notes), lower($2)) > 0)
		AND ($3::text = '' OR s.supplier_id = NULLIF($3, '')::uuid)
		AND ($4::text = '' OR s.status = NULLIF($4, '')::adjustment_status)`
	args := []any{garageID, filter.Query, filter.SupplierID, string(filter.Status)}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+supplierReturnColumns+`,
		COALESCE((SELECT sum(round(l.entered_quantity * l.unit_cost)) FROM supplier_return_lines l
			WHERE l.garage_id = s.garage_id AND l.supplier_return_id = s.id), 0)::bigint`+where+`
		ORDER BY s.created_at DESC, s.id LIMIT $5 OFFSET $6`, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.SupplierReturn, 0, filter.Limit)

	for rows.Next() {
		var credit int64

		s, err := scanSupplierReturn(rows, &credit)
		if err != nil {
			return nil, 0, err
		}

		s.CreditAmount = credit
		items = append(items, s)
	}

	return items, total, rows.Err()
}

// GoodsReceiptNumbers returns the number per goods receipt id.
func (r *PostgresRepository) GoodsReceiptNumbers(ctx context.Context, garageID string, ids []string) (map[string]string, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx,
		`SELECT id, number FROM goods_receipts WHERE garage_id = $1 AND id = ANY($2::uuid[])`, garageID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]string, len(ids))

	for rows.Next() {
		var id, number string
		if err := rows.Scan(&id, &number); err != nil {
			return nil, err
		}

		out[id] = number
	}

	return out, rows.Err()
}

// CreatePartReturn records unused parts returned from a repair order.
func (r *PostgresRepository) CreatePartReturn(ctx context.Context, p *domain.RepairOrderPartReturn) error {
	return platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO repair_order_part_returns (id, garage_id, repair_order_id, repair_order_number, stock_issue_id,
		                                       stock_issue_line_id, repair_order_line_id, part_id, unit_id, entered_quantity,
		                                       quantity, reason, unit_cost, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::numeric, $11::numeric, $12, $13::numeric, $14, $14)
		RETURNING created_at`,
		p.ID, p.GarageID, p.RepairOrderID, p.RepairOrderNumber, p.StockIssueID, p.StockIssueLineID, p.RepairOrderLineID, p.PartID,
		p.UnitID, p.EnteredQuantity, p.Quantity, p.Reason, p.UnitCost, p.CreatedBy,
	).Scan(&p.CreatedAt)
}

// ListPartReturns returns the part returns of a repair order, oldest first.
func (r *PostgresRepository) ListPartReturns(ctx context.Context, garageID, repairOrderID string) ([]domain.RepairOrderPartReturn, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT id, garage_id, repair_order_id, repair_order_number, stock_issue_id, stock_issue_line_id, repair_order_line_id,
		       part_id, unit_id, trim_scale(entered_quantity)::text, trim_scale(quantity)::text, reason,
		       trim_scale(unit_cost)::text, created_by, created_at
		FROM repair_order_part_returns WHERE garage_id = $1 AND repair_order_id = $2 ORDER BY created_at, id`,
		garageID, repairOrderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.RepairOrderPartReturn

	for rows.Next() {
		var p domain.RepairOrderPartReturn
		if err := rows.Scan(&p.ID, &p.GarageID, &p.RepairOrderID, &p.RepairOrderNumber, &p.StockIssueID, &p.StockIssueLineID,
			&p.RepairOrderLineID, &p.PartID, &p.UnitID, &p.EnteredQuantity, &p.Quantity, &p.Reason, &p.UnitCost, &p.CreatedBy,
			&p.CreatedAt); err != nil {
			return nil, err
		}

		out = append(out, p)
	}

	return out, rows.Err()
}
