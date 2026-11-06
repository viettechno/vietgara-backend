package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const orderColumns = `o.id, o.garage_id, o.number, o.status::text, o.supplier_id, to_char(o.order_date, 'YYYY-MM-DD'),
	COALESCE(to_char(o.expected_date, 'YYYY-MM-DD'), ''), o.notes, o.sent_at, o.sent_to, o.cancelled_at, o.created_by,
	o.updated_by, o.created_at, o.updated_at`

func scanOrder(row pgx.Row, extra ...any) (*domain.PurchaseOrder, error) {
	var (
		o      domain.PurchaseOrder
		status string
	)

	dest := append([]any{
		&o.ID, &o.GarageID, &o.Number, &status, &o.SupplierID, &o.OrderDate, &o.ExpectedDate, &o.Notes,
		&o.SentAt, &o.SentTo, &o.CancelledAt, &o.CreatedBy, &o.UpdatedBy, &o.CreatedAt, &o.UpdatedAt,
	}, extra...)

	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrPurchaseOrderNotFound
		}

		return nil, err
	}

	o.Status = domain.PurchaseOrderStatus(status)

	return &o, nil
}

// CreatePurchaseOrder inserts the order header and its lines.
func (r *PostgresRepository) CreatePurchaseOrder(ctx context.Context, o *domain.PurchaseOrder) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO purchase_orders (id, garage_id, number, status, supplier_id, order_date, expected_date, notes, created_by, updated_by)
		VALUES ($1, $2, $3, $4::purchase_order_status, $5, $6::date, NULLIF($7, '')::date, $8, $9, $10)
		RETURNING created_at, updated_at`,
		o.ID, o.GarageID, o.Number, string(o.Status), o.SupplierID, o.OrderDate, o.ExpectedDate, o.Notes, o.CreatedBy, o.UpdatedBy,
	).Scan(&o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return err
	}

	return r.insertOrderLines(ctx, o)
}

func (r *PostgresRepository) insertOrderLines(ctx context.Context, o *domain.PurchaseOrder) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	for i := range o.Lines {
		l := &o.Lines[i]

		_, err := q.Exec(ctx, `
			INSERT INTO purchase_order_lines (id, garage_id, purchase_order_id, position, part_id, unit_id, entered_quantity,
			                                  quantity, received_quantity, unit_cost, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8::numeric, $9::numeric, $10, $11, $12)`,
			l.ID, o.GarageID, o.ID, i, l.PartID, l.UnitID, l.EnteredQuantity, l.Quantity, orZero(l.ReceivedQuantity), l.UnitCost,
			o.UpdatedBy, o.UpdatedBy)
		if err != nil {
			return err
		}
	}

	return nil
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}

	return s
}

// FindPurchaseOrder returns the order with its lines in order.
//
//nolint:dupl // each document has its own tables and filters
func (r *PostgresRepository) FindPurchaseOrder(ctx context.Context, garageID, orderID string, forUpdate bool) (*domain.PurchaseOrder, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	lock := ""
	if forUpdate {
		lock = forUpdateClause
	}

	order, err := scanOrder(q.QueryRow(ctx, `SELECT `+orderColumns+` FROM purchase_orders o WHERE o.garage_id = $1 AND o.id = $2`+lock,
		garageID, orderID))
	if err != nil {
		return nil, err
	}

	rows, err := q.Query(ctx, `
		SELECT id, part_id, unit_id, trim_scale(entered_quantity)::text, trim_scale(quantity)::text,
		       trim_scale(received_quantity)::text, unit_cost
		FROM purchase_order_lines WHERE garage_id = $1 AND purchase_order_id = $2 ORDER BY position, id`, garageID, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var l domain.PurchaseOrderLine
		if err := rows.Scan(&l.ID, &l.PartID, &l.UnitID, &l.EnteredQuantity, &l.Quantity, &l.ReceivedQuantity, &l.UnitCost); err != nil {
			return nil, err
		}

		order.Lines = append(order.Lines, l)
	}

	return order, rows.Err()
}

// SavePurchaseOrder persists the header and replaces the lines.
func (r *PostgresRepository) SavePurchaseOrder(ctx context.Context, o *domain.PurchaseOrder) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		UPDATE purchase_orders SET status = $3::purchase_order_status, supplier_id = $4, expected_date = NULLIF($5, '')::date,
			notes = $6, sent_at = $7, sent_to = $8, cancelled_at = $9, updated_by = $10
		WHERE garage_id = $1 AND id = $2
		RETURNING updated_at`,
		o.GarageID, o.ID, string(o.Status), o.SupplierID, o.ExpectedDate, o.Notes, o.SentAt, o.SentTo, o.CancelledAt, o.UpdatedBy,
	).Scan(&o.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrPurchaseOrderNotFound
		}

		return err
	}

	if _, err := q.Exec(ctx, `DELETE FROM purchase_order_lines WHERE garage_id = $1 AND purchase_order_id = $2`, o.GarageID, o.ID); err != nil {
		return err
	}

	return r.insertOrderLines(ctx, o)
}

// ListPurchaseOrders returns one page of orders, newest first, with the total
// of their lines and the supplier's name.
//
//nolint:dupl // each document has its own tables and filters
func (r *PostgresRepository) ListPurchaseOrders(ctx context.Context, garageID string, f usecase.PurchaseOrderFilter) ([]*domain.PurchaseOrder, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := ` FROM purchase_orders o JOIN suppliers s ON s.garage_id = o.garage_id AND s.id = o.supplier_id
		WHERE o.garage_id = $1
		AND ($2::text = '' OR strpos(lower(o.number || ' ' || s.name || ' ' || o.notes), lower($2)) > 0)
		AND ($3::text = '' OR o.supplier_id = NULLIF($3, '')::uuid)
		AND ($4::text = '' OR o.status = NULLIF($4, '')::purchase_order_status)`
	args := []any{garageID, f.Query, f.SupplierID, string(f.Status)}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+orderColumns+`,
		COALESCE((SELECT sum(round(l.entered_quantity * l.unit_cost)) FROM purchase_order_lines l
			WHERE l.garage_id = o.garage_id AND l.purchase_order_id = o.id), 0)::bigint`+where+`
		ORDER BY o.created_at DESC, o.id LIMIT $5 OFFSET $6`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.PurchaseOrder, 0, f.Limit)

	for rows.Next() {
		var amount int64

		order, err := scanOrder(rows, &amount)
		if err != nil {
			return nil, 0, err
		}

		order.TotalAmount = amount
		items = append(items, order)
	}

	return items, total, rows.Err()
}

// OnOrder returns, per part, the base-unit quantity still to be delivered by
// draft, sent and partially received orders.
func (r *PostgresRepository) OnOrder(ctx context.Context, garageID string, partIDs []string) (map[string]string, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `
		SELECT l.part_id, trim_scale(sum(GREATEST(l.quantity - l.received_quantity, 0)))::text
		FROM purchase_order_lines l JOIN purchase_orders o ON o.garage_id = l.garage_id AND o.id = l.purchase_order_id
		WHERE l.garage_id = $1 AND l.part_id = ANY($2::uuid[]) AND o.status IN ('draft', 'sent', 'partially_received')
		GROUP BY l.part_id HAVING sum(GREATEST(l.quantity - l.received_quantity, 0)) > 0`, garageID, partIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}

	for rows.Next() {
		var partID, quantity string
		if err := rows.Scan(&partID, &quantity); err != nil {
			return nil, err
		}

		out[partID] = quantity
	}

	return out, rows.Err()
}

// HasDraftReceipt reports whether a draft goods receipt of the order exists.
func (r *PostgresRepository) HasDraftReceipt(ctx context.Context, garageID, orderID string) (bool, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	var open bool

	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM goods_receipts WHERE garage_id = $1 AND purchase_order_id = $2 AND status = 'draft')`,
		garageID, orderID).Scan(&open)

	return open, err
}
