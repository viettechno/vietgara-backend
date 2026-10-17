package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const receiptColumns = `r.id, r.garage_id, r.number, r.status::text, r.default_supplier_id, to_char(r.receipt_date, 'YYYY-MM-DD'),
	r.delivery_note_number, r.notes, r.completed_at, r.cancelled_at, r.created_by, r.updated_by, r.created_at, r.updated_at`

func scanReceipt(row pgx.Row) (*domain.GoodsReceipt, error) {
	var (
		r      domain.GoodsReceipt
		status string
	)

	err := row.Scan(&r.ID, &r.GarageID, &r.Number, &status, &r.DefaultSupplierID, &r.ReceiptDate, &r.DeliveryNoteNumber,
		&r.Notes, &r.CompletedAt, &r.CancelledAt, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrGoodsReceiptNotFound
		}

		return nil, err
	}

	r.Status = domain.GoodsReceiptStatus(status)

	return &r, nil
}

// CreateGoodsReceipt inserts the receipt header and its lines.
func (r *PostgresRepository) CreateGoodsReceipt(ctx context.Context, receipt *domain.GoodsReceipt) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO goods_receipts (id, garage_id, number, status, default_supplier_id, receipt_date, delivery_note_number,
		                            notes, created_by, updated_by)
		VALUES ($1, $2, $3, $4::goods_receipt_status, $5, $6::date, $7, $8, $9, $10)
		RETURNING created_at, updated_at`,
		receipt.ID, receipt.GarageID, receipt.Number, string(receipt.Status), receipt.DefaultSupplierID, receipt.ReceiptDate,
		receipt.DeliveryNoteNumber, receipt.Notes, receipt.CreatedBy, receipt.UpdatedBy,
	).Scan(&receipt.CreatedAt, &receipt.UpdatedAt)
	if err != nil {
		return err
	}

	return r.insertReceiptLines(ctx, receipt)
}

func (r *PostgresRepository) insertReceiptLines(ctx context.Context, receipt *domain.GoodsReceipt) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	for i, l := range receipt.Lines {
		_, err := q.Exec(ctx, `
			INSERT INTO goods_receipt_lines (id, garage_id, goods_receipt_id, position, part_id, unit_id, entered_quantity,
			                                 quantity, supplier_id, unit_cost, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9, $10, $11, $12)`,
			l.ID, receipt.GarageID, receipt.ID, i, l.PartID, l.UnitID, l.EnteredQuantity, l.Quantity, l.SupplierID,
			l.UnitCost, receipt.UpdatedBy, receipt.UpdatedBy)
		if err != nil {
			return err
		}
	}

	return nil
}

// FindGoodsReceipt returns the receipt with its lines in order.
func (r *PostgresRepository) FindGoodsReceipt(ctx context.Context, garageID, receiptID string, forUpdate bool) (*domain.GoodsReceipt, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	lock := ""
	if forUpdate {
		lock = forUpdateClause
	}

	receipt, err := scanReceipt(q.QueryRow(ctx,
		`SELECT `+receiptColumns+` FROM goods_receipts r WHERE r.garage_id = $1 AND r.id = $2`+lock, garageID, receiptID))
	if err != nil {
		return nil, err
	}

	rows, err := q.Query(ctx, `
		SELECT id, part_id, unit_id, trim_scale(entered_quantity)::text, trim_scale(quantity)::text, supplier_id, unit_cost
		FROM goods_receipt_lines WHERE garage_id = $1 AND goods_receipt_id = $2 ORDER BY position, id`, garageID, receiptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var l domain.GoodsReceiptLine
		if err := rows.Scan(&l.ID, &l.PartID, &l.UnitID, &l.EnteredQuantity, &l.Quantity, &l.SupplierID, &l.UnitCost); err != nil {
			return nil, err
		}

		receipt.Lines = append(receipt.Lines, l)
	}

	return receipt, rows.Err()
}

// SaveGoodsReceipt persists the header and replaces the lines.
func (r *PostgresRepository) SaveGoodsReceipt(ctx context.Context, receipt *domain.GoodsReceipt) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		UPDATE goods_receipts SET status = $3::goods_receipt_status, default_supplier_id = $4, receipt_date = $5::date,
			delivery_note_number = $6, notes = $7, completed_at = $8, cancelled_at = $9, updated_by = $10
		WHERE garage_id = $1 AND id = $2
		RETURNING updated_at`,
		receipt.GarageID, receipt.ID, string(receipt.Status), receipt.DefaultSupplierID, receipt.ReceiptDate,
		receipt.DeliveryNoteNumber, receipt.Notes, receipt.CompletedAt, receipt.CancelledAt, receipt.UpdatedBy,
	).Scan(&receipt.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrGoodsReceiptNotFound
		}

		return err
	}

	if _, err := q.Exec(ctx, `DELETE FROM goods_receipt_lines WHERE garage_id = $1 AND goods_receipt_id = $2`,
		receipt.GarageID, receipt.ID); err != nil {
		return err
	}

	return r.insertReceiptLines(ctx, receipt)
}

// ListGoodsReceipts returns one page of receipts, newest first, with the
// total of their lines.
func (r *PostgresRepository) ListGoodsReceipts(ctx context.Context, garageID string, filter usecase.GoodsReceiptFilter) ([]*domain.GoodsReceipt, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := ` FROM goods_receipts r WHERE r.garage_id = $1
		AND ($2::text = '' OR strpos(lower(r.number || ' ' || r.delivery_note_number || ' ' || r.notes), lower($2)) > 0)
		AND ($3::text = '' OR EXISTS (SELECT 1 FROM goods_receipt_lines l
			WHERE l.garage_id = r.garage_id AND l.goods_receipt_id = r.id AND l.supplier_id = NULLIF($3, '')::uuid))
		AND ($4::text = '' OR r.status = NULLIF($4, '')::goods_receipt_status)`
	args := []any{garageID, filter.Query, filter.SupplierID, string(filter.Status)}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+receiptColumns+`,
		COALESCE((SELECT sum(round(l.entered_quantity * l.unit_cost)) FROM goods_receipt_lines l
			WHERE l.garage_id = r.garage_id AND l.goods_receipt_id = r.id), 0)::bigint`+where+`
		ORDER BY r.created_at DESC, r.id LIMIT $5 OFFSET $6`, append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.GoodsReceipt, 0, filter.Limit)

	for rows.Next() {
		var (
			receipt domain.GoodsReceipt
			status  string
		)

		if err := rows.Scan(&receipt.ID, &receipt.GarageID, &receipt.Number, &status, &receipt.DefaultSupplierID,
			&receipt.ReceiptDate, &receipt.DeliveryNoteNumber, &receipt.Notes, &receipt.CompletedAt, &receipt.CancelledAt,
			&receipt.CreatedBy, &receipt.UpdatedBy, &receipt.CreatedAt, &receipt.UpdatedAt, &receipt.TotalAmount); err != nil {
			return nil, 0, err
		}

		receipt.Status = domain.GoodsReceiptStatus(status)
		items = append(items, &receipt)
	}

	return items, total, rows.Err()
}
