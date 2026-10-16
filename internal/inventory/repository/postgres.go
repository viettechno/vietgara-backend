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
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const pgUniqueViolation = "23505"

// PostgresRepository implements usecase.Repository over the shared pgx
// pool.
type PostgresRepository struct {
	pool *platformdb.Pool
}

// NewPostgresRepository returns an inventory repository backed by pool.
func NewPostgresRepository(pool *platformdb.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Balances returns the quantity on hand per part; with ids empty, of every
// part that has a balance.
func (r *PostgresRepository) Balances(ctx context.Context, garageID string, ids []string) (map[string]string, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	if ids == nil {
		ids = []string{}
	}

	rows, err := q.Query(ctx, `
		SELECT item_id, trim_scale(quantity_on_hand)::text FROM stock_balance
		WHERE garage_id = $1 AND (cardinality($2::uuid[]) = 0 OR item_id = ANY($2::uuid[]))`, garageID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}

	for rows.Next() {
		var id, quantity string
		if err := rows.Scan(&id, &quantity); err != nil {
			return nil, err
		}

		out[id] = quantity
	}

	return out, rows.Err()
}

// RecordTransaction inserts tx and atomically applies its effect to
// stock_balance in the same call.
func (r *PostgresRepository) RecordTransaction(ctx context.Context, transaction *domain.StockTransaction) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	err := q.QueryRow(ctx, `
		INSERT INTO stock_transactions (id, garage_id, item_id, type, quantity, unit_cost, unit_id, entered_quantity,
		                                ref_type, ref_id, created_by)
		VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7, $8::numeric, $9, $10, $11)
		RETURNING created_at`,
		transaction.ID, transaction.GarageID, transaction.PartID, string(transaction.Type), transaction.Quantity,
		transaction.UnitCost, transaction.UnitID, transaction.EnteredQuantity, string(transaction.RefType),
		transaction.RefID, transaction.CreatedBy,
	).Scan(&transaction.CreatedAt)
	if err != nil {
		if isUniqueViolation(err, "idx_stock_transactions_repair_order_item_unique") {
			return domain.ErrAlreadyIssued
		}

		return err
	}

	delta := "+"
	if transaction.Type == domain.TransactionOut {
		delta = "-"
	}

	_, err = q.Exec(ctx, `
		INSERT INTO stock_balance (garage_id, item_id, quantity_on_hand)
		VALUES ($1, $2, `+delta+`$3::numeric)
		ON CONFLICT (garage_id, item_id)
		DO UPDATE SET quantity_on_hand = stock_balance.quantity_on_hand `+delta+` $3::numeric, updated_at = now()`,
		transaction.GarageID, transaction.PartID, transaction.Quantity,
	)

	return err
}

const transactionColumns = `t.id, t.item_id, t.type, trim_scale(t.quantity)::text, trim_scale(t.unit_cost)::text, t.unit_id,
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
		AND ($2::uuid IS NULL OR t.item_id = $2::uuid)
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

	items := make([]*domain.StockTransaction, 0, filter.Limit)

	for rows.Next() {
		item, err := scanTransaction(rows)
		if err != nil {
			return nil, 0, err
		}

		items = append(items, item)
	}

	return items, total, rows.Err()
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// isUniqueViolation reports a unique-constraint error on a constraint
// whose name contains fragment.
func isUniqueViolation(err error, fragment string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && strings.Contains(pgErr.ConstraintName, fragment)
}

// MovementsReference reports whether a stock movement is of the part or was
// entered in the unit, for the Catalogue's delete-or-deactivate rule
// (FR-CAT-07). partID or unitID may be "" to skip it.
func (r *PostgresRepository) MovementsReference(ctx context.Context, garageID, partID, unitID string) (bool, error) {
	var exists bool

	err := platformdb.QuerierFromContext(ctx, r.pool).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM stock_transactions
			WHERE garage_id = $1 AND (($2::text <> '' AND item_id = NULLIF($2, '')::uuid)
				OR ($3::text <> '' AND unit_id = NULLIF($3, '')::uuid)))`,
		garageID, partID, unitID).Scan(&exists)

	return exists, err
}
