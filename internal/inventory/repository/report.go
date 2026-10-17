package repository

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PurchasesReport returns the lines of completed receipts in the range, newest
// first. The cost per base unit and the amount are rounded to whole VND.
func (r *PostgresRepository) PurchasesReport(ctx context.Context, garageID string, filter usecase.PurchaseFilter) ([]domain.PurchaseLine, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT r.id, r.number, to_char(r.receipt_date, 'YYYY-MM-DD'), l.supplier_id, l.part_id, trim_scale(l.quantity)::text,
		       round(l.unit_cost::numeric * l.entered_quantity / l.quantity)::bigint,
		       round(l.entered_quantity * l.unit_cost)::bigint
		FROM goods_receipt_lines l
		JOIN goods_receipts r ON r.garage_id = l.garage_id AND r.id = l.goods_receipt_id
		WHERE r.garage_id = $1 AND r.status = 'completed' AND l.supplier_id IS NOT NULL
		  AND ($2::text = '' OR l.supplier_id = NULLIF($2, '')::uuid)
		  AND ($3::text = '' OR r.receipt_date >= NULLIF($3, '')::date)
		  AND ($4::text = '' OR r.receipt_date < NULLIF($4, '')::date)
		ORDER BY r.receipt_date DESC, r.number DESC, l.position`,
		garageID, filter.SupplierID, filter.From, filter.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lines := []domain.PurchaseLine{}

	for rows.Next() {
		var l domain.PurchaseLine
		if err := rows.Scan(&l.GoodsReceiptID, &l.GoodsReceiptNumber, &l.ReceiptDate, &l.SupplierID, &l.PartID, &l.Quantity,
			&l.UnitCost, &l.Amount); err != nil {
			return nil, err
		}

		lines = append(lines, l)
	}

	return lines, rows.Err()
}

// StockValuation returns every part's quantity and average cost after its
// last movement before upTo.
func (r *PostgresRepository) StockValuation(ctx context.Context, garageID string, upTo time.Time) ([]usecase.ValuationRow, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT DISTINCT ON (t.part_id) t.part_id, trim_scale(t.quantity_after)::text, trim_scale(t.average_cost_after)::text
		FROM stock_transactions t
		WHERE t.garage_id = $1 AND t.created_at < $2
		ORDER BY t.part_id, t.seq DESC`, garageID, upTo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []usecase.ValuationRow{}

	for rows.Next() {
		var v usecase.ValuationRow
		if err := rows.Scan(&v.PartID, &v.Quantity, &v.AverageCost); err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, rows.Err()
}
