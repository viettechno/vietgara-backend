// Package repository implements the persistence layer of the Inventory
// module on top of the shared Postgres pool. Every query filters on
// garage_id explicitly on top of Row-Level Security. The tables hold part
// ids only; the part itself lives in the Catalogue module. See: Database
// Design Section 5.
package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// forUpdateClause locks the selected row until the transaction ends.
const forUpdateClause = " FOR UPDATE"

// EventPublisher writes an event to the outbox in the caller's transaction.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, userID string, payload any) error
}

// EventStockLow is published when a movement takes a part from above its
// reorder threshold to at or below it (FR-INV-04): once per crossing, since
// the quantities before and after the movement decide it.
const EventStockLow = "stock.low"

// PostgresRepository implements usecase.Repository over the shared pgx
// pool.
type PostgresRepository struct {
	pool   *platformdb.Pool
	events EventPublisher
}

// WithEvents makes the repository publish stock.low.
func (r *PostgresRepository) WithEvents(events EventPublisher) *PostgresRepository {
	r.events = events

	return r
}

// NewPostgresRepository returns an inventory repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Balances returns the stock position per part; with ids empty, of every
// part that has a balance or a reservation. The reserved quantity sums the
// lines of the part's draft stock issues (FR-INV-20).
func (r *PostgresRepository) Balances(ctx context.Context, garageID string, ids []string) (map[string]usecase.Balance, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	if ids == nil {
		ids = []string{}
	}

	rows, err := q.Query(ctx, `
		WITH bal AS (
			SELECT part_id, quantity_on_hand, average_cost FROM stock_balance
			WHERE garage_id = $1 AND (cardinality($2::uuid[]) = 0 OR part_id = ANY($2::uuid[]))
		), reserved AS (
			SELECT l.part_id, sum(l.quantity) AS quantity
			FROM stock_issue_lines l
			JOIN stock_issues i ON i.garage_id = l.garage_id AND i.id = l.stock_issue_id
			WHERE l.garage_id = $1 AND i.status = 'draft' AND (cardinality($2::uuid[]) = 0 OR l.part_id = ANY($2::uuid[]))
			GROUP BY l.part_id
		)
		SELECT COALESCE(bal.part_id, reserved.part_id),
		       trim_scale(COALESCE(bal.quantity_on_hand, 0))::text,
		       trim_scale(COALESCE(reserved.quantity, 0))::text,
		       trim_scale(COALESCE(bal.average_cost, 0))::text
		FROM bal FULL JOIN reserved ON reserved.part_id = bal.part_id`, garageID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]usecase.Balance{}

	for rows.Next() {
		var (
			id string
			b  usecase.Balance
		)

		if err := rows.Scan(&id, &b.OnHand, &b.Reserved, &b.AverageCost); err != nil {
			return nil, err
		}

		out[id] = b
	}

	return out, rows.Err()
}

// LockBalances locks the balance rows of the parts until the transaction ends.
func (r *PostgresRepository) LockBalances(ctx context.Context, garageID string, partIDs []string) error {
	if len(partIDs) == 0 {
		return nil
	}

	_, err := platformdb.QuerierFromContext(ctx, r.pool).Exec(ctx, `
		SELECT 1 FROM stock_balance WHERE garage_id = $1 AND part_id = ANY($2::uuid[]) ORDER BY part_id FOR UPDATE`,
		garageID, partIDs)

	return err
}

// applyBalance upserts the part's balance for transaction and returns the
// quantity on hand and the average cost afterwards. The weighted-average
// cost is computed in the database so concurrent movements of one part stay
// consistent:
//   - an IN with a cost: (on hand * average + quantity * cost) / (on hand +
//     quantity), or just the cost while on hand is zero or negative;
//   - the OUT that reverses a goods receipt line (it carries the line's
//     cost): the same formula run backwards, never below zero;
//   - any other movement leaves the average alone.
func (r *PostgresRepository) applyBalance(ctx context.Context, t *domain.StockTransaction) (quantity, average string, err error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	var query string

	switch {
	case t.Type == domain.TransactionIn:
		query = `
			INSERT INTO stock_balance (garage_id, part_id, quantity_on_hand, average_cost)
			VALUES ($1, $2, $3::numeric, COALESCE(round($4::numeric, 4), 0))
			ON CONFLICT (garage_id, part_id) DO UPDATE SET
				quantity_on_hand = stock_balance.quantity_on_hand + $3::numeric,
				average_cost = CASE
					WHEN $4::numeric IS NULL THEN stock_balance.average_cost
					WHEN stock_balance.quantity_on_hand <= 0 THEN round($4::numeric, 4)
					ELSE round((stock_balance.quantity_on_hand * stock_balance.average_cost + $3::numeric * $4::numeric)
						/ (stock_balance.quantity_on_hand + $3::numeric), 4)
				END,
				updated_at = now()
			RETURNING trim_scale(quantity_on_hand)::text, trim_scale(average_cost)::text`
	case t.RefType == domain.RefGoodsReceipt && t.UnitCost != nil:
		query = `
			INSERT INTO stock_balance (garage_id, part_id, quantity_on_hand, average_cost)
			VALUES ($1, $2, -$3::numeric, 0)
			ON CONFLICT (garage_id, part_id) DO UPDATE SET
				quantity_on_hand = stock_balance.quantity_on_hand - $3::numeric,
				average_cost = CASE
					WHEN stock_balance.quantity_on_hand - $3::numeric <= 0 THEN stock_balance.average_cost
					ELSE greatest(round((stock_balance.quantity_on_hand * stock_balance.average_cost - $3::numeric * $4::numeric)
						/ (stock_balance.quantity_on_hand - $3::numeric), 4), 0)
				END,
				updated_at = now()
			RETURNING trim_scale(quantity_on_hand)::text, trim_scale(average_cost)::text`
	default:
		query = `
			INSERT INTO stock_balance (garage_id, part_id, quantity_on_hand, average_cost)
			VALUES ($1, $2, -$3::numeric, 0)
			ON CONFLICT (garage_id, part_id) DO UPDATE SET
				quantity_on_hand = stock_balance.quantity_on_hand - $3::numeric, updated_at = now()
			RETURNING trim_scale(quantity_on_hand)::text, trim_scale(average_cost)::text`
	}

	args := []any{t.GarageID, t.PartID, t.Quantity}
	if strings.Contains(query, "$4") {
		args = append(args, t.UnitCost)
	}

	err = q.QueryRow(ctx, query, args...).Scan(&quantity, &average)

	return quantity, average, err
}

// RecordTransaction updates the part's balance and inserts the movement,
// remembering the quantity and average cost right after it (for the
// valuation report). An OUT without a cost is stamped with the average cost
// of the moment, when there is one.
func (r *PostgresRepository) RecordTransaction(ctx context.Context, transaction *domain.StockTransaction) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	quantity, average, err := r.applyBalance(ctx, transaction)
	if err != nil {
		return err
	}

	// A movement without a cost is valued at the average of the moment: an
	// issue always, an adjustment's IN as well (which left the average alone).
	if transaction.UnitCost == nil && !domain.IsZero(average) &&
		(transaction.Type == domain.TransactionOut || transaction.RefType.IsAdjustment()) {
		transaction.UnitCost = &average
	}

	transaction.QuantityAfter = quantity

	if err := q.QueryRow(ctx, `
		INSERT INTO stock_transactions (id, garage_id, part_id, type, quantity, unit_cost, unit_id, entered_quantity,
		                                ref_type, ref_id, supplier_id, quantity_after, average_cost_after, created_by, reason)
		VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7, $8::numeric, $9, $10, $11, $12::numeric, $13::numeric, $14, $15)
		RETURNING created_at`,
		transaction.ID, transaction.GarageID, transaction.PartID, string(transaction.Type), transaction.Quantity,
		transaction.UnitCost, transaction.UnitID, transaction.EnteredQuantity, string(transaction.RefType),
		transaction.RefID, transaction.SupplierID, quantity, average, transaction.CreatedBy, transaction.Reason,
	).Scan(&transaction.CreatedAt); err != nil {
		return err
	}

	return r.publishIfLow(ctx, transaction, quantity)
}

// publishIfLow tells Notification that an issue took the part to or under its
// reorder threshold. A part with no threshold (zero) never alerts, and a
// movement that left the part under the threshold it was already under
// alerts nobody.
func (r *PostgresRepository) publishIfLow(ctx context.Context, t *domain.StockTransaction, after string) error {
	if r.events == nil || t.Type != domain.TransactionOut {
		return nil
	}

	var name, sku, threshold string

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		SELECT name, COALESCE(sku, ''), trim_scale(reorder_threshold)::text FROM parts WHERE garage_id = $1 AND id = $2`,
		t.GarageID, t.PartID).Scan(&name, &sku, &threshold)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		return err
	}

	before, ok := domain.AddQuantities(after, t.Quantity)
	if !ok || domain.IsZero(threshold) || !domain.IsLow(after, threshold) || domain.IsLow(before, threshold) {
		return nil
	}

	return r.events.Publish(ctx, EventStockLow, t.GarageID, "", map[string]any{
		"part_id": t.PartID, "part_name": name, "sku": sku, "quantity": after, "threshold": threshold,
	})
}

// transactionColumns and transactionJoins select a movement with the number of
// the document behind it; the joins only add columns, never rows.
const transactionColumns = `t.id, t.part_id, t.type, trim_scale(t.quantity)::text, trim_scale(t.unit_cost)::text, t.unit_id,
	trim_scale(t.entered_quantity)::text, t.ref_type, t.ref_id, t.created_by, t.created_at, t.reason,
	trim_scale(t.quantity_after)::text,
	COALESCE(gr.number, si.number, sc.number, wo.number, sr.number, '')`

const transactionJoins = `
	LEFT JOIN goods_receipts gr ON t.ref_type = 'goods_receipt' AND gr.garage_id = t.garage_id AND gr.id = t.ref_id
	LEFT JOIN stock_issues si ON t.ref_type = 'stock_issue' AND si.garage_id = t.garage_id AND si.id = t.ref_id
	LEFT JOIN stock_counts sc ON t.ref_type = 'stock_count' AND sc.garage_id = t.garage_id AND sc.id = t.ref_id
	LEFT JOIN write_offs wo ON t.ref_type = 'write_off' AND wo.garage_id = t.garage_id AND wo.id = t.ref_id
	LEFT JOIN supplier_returns sr ON t.ref_type = 'supplier_return' AND sr.garage_id = t.garage_id AND sr.id = t.ref_id`

func scanTransaction(row pgx.Row) (*domain.StockTransaction, error) {
	var (
		transaction domain.StockTransaction
		txType      string
		refType     string
	)

	err := row.Scan(&transaction.ID, &transaction.PartID, &txType, &transaction.Quantity, &transaction.UnitCost,
		&transaction.UnitID, &transaction.EnteredQuantity, &refType, &transaction.RefID, &transaction.CreatedBy,
		&transaction.CreatedAt, &transaction.Reason, &transaction.QuantityAfter, &transaction.RefNumber)
	if err != nil {
		return nil, err
	}

	transaction.Type, transaction.RefType = domain.TransactionType(txType), domain.RefType(refType)

	return &transaction, nil
}

// ListTransactions returns one page of the garage's stock transactions
// (FR-INV-05), newest first.
func (r *PostgresRepository) ListTransactions(ctx context.Context, garageID string, filter usecase.TransactionFilter) ([]*domain.StockTransaction, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	where := ` FROM stock_transactions t WHERE t.garage_id = $1
		AND ($2::uuid IS NULL OR t.part_id = $2::uuid)
		AND ($3::stock_transaction_type IS NULL OR t.type = $3::stock_transaction_type)
		AND ($4::stock_transaction_ref_type IS NULL OR t.ref_type = $4::stock_transaction_ref_type)`
	args := []any{garageID, nullable(filter.PartID), nullable(string(filter.Type)), nullable(string(filter.RefType))}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+transactionColumns+` FROM stock_transactions t`+transactionJoins+`
		WHERE t.garage_id = $1
		AND ($2::uuid IS NULL OR t.part_id = $2::uuid)
		AND ($3::stock_transaction_type IS NULL OR t.type = $3::stock_transaction_type)
		AND ($4::stock_transaction_ref_type IS NULL OR t.ref_type = $4::stock_transaction_ref_type)
		ORDER BY t.created_at DESC, t.seq DESC LIMIT $5 OFFSET $6`,
		append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	parts := make([]*domain.StockTransaction, 0, filter.Limit)

	for rows.Next() {
		part, err := scanTransaction(rows)
		if err != nil {
			return nil, 0, err
		}

		parts = append(parts, part)
	}

	return parts, total, rows.Err()
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// MovementsReference reports whether a stock movement is of the part or was
// entered in the unit, for the Catalogue's delete-or-deactivate rule
// (FR-CAT-07). partID or unitID may be "" to skip it.
func (r *PostgresRepository) MovementsReference(ctx context.Context, garageID, partID, unitID string) (bool, error) {
	var exists bool

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM stock_transactions
			WHERE garage_id = $1 AND (($2::text <> '' AND part_id = NULLIF($2, '')::uuid)
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))
		OR EXISTS (
			SELECT 1 FROM goods_receipt_lines
			WHERE garage_id = $1 AND (($2::text <> '' AND part_id = NULLIF($2, '')::uuid)
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))
		OR EXISTS (
			SELECT 1 FROM stock_issue_lines
			WHERE garage_id = $1 AND (($2::text <> '' AND part_id = NULLIF($2, '')::uuid)
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))
		OR EXISTS (
			SELECT 1 FROM stock_count_lines
			WHERE garage_id = $1 AND (($2::text <> '' AND part_id = NULLIF($2, '')::uuid)
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))
		OR EXISTS (
			SELECT 1 FROM write_off_lines
			WHERE garage_id = $1 AND (($2::text <> '' AND part_id = NULLIF($2, '')::uuid)
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))
		OR EXISTS (
			SELECT 1 FROM supplier_return_lines
			WHERE garage_id = $1 AND (($2::text <> '' AND part_id = NULLIF($2, '')::uuid)
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))
		OR EXISTS (
			SELECT 1 FROM repair_order_part_returns
			WHERE garage_id = $1 AND (($2::text <> '' AND part_id = NULLIF($2, '')::uuid)
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))`,
		garageID, partID, unitID).Scan(&exists)

	return exists, err
}
