// Package repository implements the persistence layer of the Inventory
// module on top of the shared Postgres pool. Every query filters on
// garage_id explicitly on top of Row-Level Security. The tables hold part
// ids only; the part itself lives in the Catalogue module. See: Database
// Design Section 5.
package repository

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresRepository implements usecase.Repository over the shared pgx
// pool.
type PostgresRepository struct {
	pool *platformdb.Pool
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

	if transaction.Type == domain.TransactionOut && transaction.UnitCost == nil && !domain.IsZero(average) {
		transaction.UnitCost = &average
	}

	return q.QueryRow(ctx, `
		INSERT INTO stock_transactions (id, garage_id, part_id, type, quantity, unit_cost, unit_id, entered_quantity,
		                                ref_type, ref_id, supplier_id, quantity_after, average_cost_after, created_by)
		VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7, $8::numeric, $9, $10, $11, $12::numeric, $13::numeric, $14)
		RETURNING created_at`,
		transaction.ID, transaction.GarageID, transaction.PartID, string(transaction.Type), transaction.Quantity,
		transaction.UnitCost, transaction.UnitID, transaction.EnteredQuantity, string(transaction.RefType),
		transaction.RefID, transaction.SupplierID, quantity, average, transaction.CreatedBy,
	).Scan(&transaction.CreatedAt)
}

const transactionColumns = `t.id, t.part_id, t.type, trim_scale(t.quantity)::text, trim_scale(t.unit_cost)::text, t.unit_id,
	trim_scale(t.entered_quantity)::text, t.ref_type, t.ref_id, t.created_by, t.created_at`

func scanTransaction(row pgx.Row) (*domain.StockTransaction, error) {
	var (
		transaction domain.StockTransaction
		txType      string
		refType     string
	)

	err := row.Scan(&transaction.ID, &transaction.PartID, &txType, &transaction.Quantity, &transaction.UnitCost,
		&transaction.UnitID, &transaction.EnteredQuantity, &refType, &transaction.RefID, &transaction.CreatedBy,
		&transaction.CreatedAt)
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
		AND ($3::stock_transaction_type IS NULL OR t.type = $3::stock_transaction_type)`
	args := []any{garageID, nullable(filter.PartID), nullable(string(filter.Type))}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+transactionColumns+where+`
		ORDER BY t.created_at DESC, t.id LIMIT $4 OFFSET $5`,
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
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))`,
		garageID, partID, unitID).Scan(&exists)

	return exists, err
}
