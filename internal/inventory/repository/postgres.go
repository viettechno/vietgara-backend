// Package repository implements the persistence layer of the Inventory
// module on top of the shared Postgres pool. Every query filters on
// garage_id explicitly on top of Row-Level Security and skips
// soft-deleted items. See: Database Design Section 5.
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

const itemColumns = `i.id, i.garage_id, i.sku, i.name, i.unit, i.reorder_threshold, i.created_by, i.updated_by,
	i.created_at, i.updated_at, COALESCE(sb.quantity_on_hand, 0)::text`

const itemFrom = ` FROM items i LEFT JOIN stock_balance sb ON sb.garage_id = i.garage_id AND sb.item_id = i.id`

func scanItem(row pgx.Row) (*domain.Item, error) {
	var item domain.Item

	err := row.Scan(&item.ID, &item.GarageID, &item.SKU, &item.Name, &item.Unit, &item.ReorderThreshold,
		&item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt, &item.QuantityOnHand)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrItemNotFound
		}

		return nil, err
	}

	return &item, nil
}

// CreateItem inserts item. A duplicate SKU returns domain.ErrSKUTaken.
func (r *PostgresRepository) CreateItem(ctx context.Context, item *domain.Item) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	row := q.QueryRow(ctx, `
		INSERT INTO items (id, garage_id, sku, name, unit, reorder_threshold, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at`,
		item.ID, item.GarageID, item.SKU, item.Name, item.Unit, item.ReorderThreshold, item.CreatedBy, item.UpdatedBy,
	)

	err := row.Scan(&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err, "idx_items_garage_sku_unique") {
			return domain.ErrSKUTaken
		}

		return err
	}

	return nil
}

// FindItemByID returns the live item with its current quantity on hand,
// or domain.ErrItemNotFound.
func (r *PostgresRepository) FindItemByID(ctx context.Context, garageID, itemID string) (*domain.Item, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	return scanItem(q.QueryRow(ctx, `SELECT `+itemColumns+itemFrom+`
		WHERE i.garage_id = $1 AND i.id = $2 AND i.deleted_at IS NULL`, garageID, itemID))
}

// UpdateItem persists the editable fields of item. A duplicate SKU
// returns domain.ErrSKUTaken.
func (r *PostgresRepository) UpdateItem(ctx context.Context, item *domain.Item) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	row := q.QueryRow(ctx, `
		UPDATE items SET sku = $3, name = $4, unit = $5, reorder_threshold = $6, updated_by = $7
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING updated_at`,
		item.GarageID, item.ID, item.SKU, item.Name, item.Unit, item.ReorderThreshold, item.UpdatedBy,
	)

	err := row.Scan(&item.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrItemNotFound
		}

		if isUniqueViolation(err, "idx_items_garage_sku_unique") {
			return domain.ErrSKUTaken
		}

		return err
	}

	return nil
}

// ListItems returns one page of live items with their quantity on hand,
// newest first.
func (r *PostgresRepository) ListItems(ctx context.Context, garageID string, filter usecase.ListFilter) ([]*domain.Item, int, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	pattern := containsPattern(filter.Query)

	where := ` WHERE i.garage_id = $1 AND i.deleted_at IS NULL AND ($2::text = '' OR i.sku ILIKE $2 OR i.name ILIKE $2)`
	args := []any{garageID, pattern}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+itemFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+itemColumns+itemFrom+where+` ORDER BY i.created_at DESC, i.id LIMIT $3 OFFSET $4`,
		append(args, filter.Limit, filter.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*domain.Item, 0, filter.Limit)

	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, 0, err
		}

		items = append(items, item)
	}

	return items, total, rows.Err()
}

// SoftDeleteItem marks the item deleted, or returns domain.ErrItemNotFound.
func (r *PostgresRepository) SoftDeleteItem(ctx context.Context, garageID, itemID, deletedBy string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	tag, err := q.Exec(ctx, `
		UPDATE items SET deleted_at = now(), updated_by = $3
		WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL`,
		garageID, itemID, nullable(deletedBy))
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrItemNotFound
	}

	return nil
}

// RecordTransaction inserts tx and atomically applies its effect to
// stock_balance in the same call.
func (r *PostgresRepository) RecordTransaction(ctx context.Context, transaction *domain.StockTransaction) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	_, err := q.Exec(ctx, `
		INSERT INTO stock_transactions (id, garage_id, item_id, type, quantity, unit_cost, ref_type, ref_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		transaction.ID, transaction.GarageID, transaction.ItemID, string(transaction.Type), transaction.Quantity,
		transaction.UnitCost, string(transaction.RefType), transaction.RefID, transaction.CreatedBy,
	)
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
		transaction.GarageID, transaction.ItemID, transaction.Quantity,
	)

	return err
}

const transactionColumns = `t.id, t.item_id, i.sku, i.name, t.type, t.quantity, t.unit_cost, t.ref_type, t.ref_id, t.created_by, t.created_at`

const transactionFrom = ` FROM stock_transactions t JOIN items i ON i.garage_id = t.garage_id AND i.id = t.item_id`

func scanTransaction(row pgx.Row) (*domain.StockTransaction, error) {
	var (
		transaction domain.StockTransaction
		txType      string
		refType     string
	)

	err := row.Scan(&transaction.ID, &transaction.ItemID, &transaction.ItemSKU, &transaction.ItemName, &txType,
		&transaction.Quantity, &transaction.UnitCost, &refType, &transaction.RefID, &transaction.CreatedBy, &transaction.CreatedAt)
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

	where := ` WHERE t.garage_id = $1
		AND ($2::uuid IS NULL OR t.item_id = $2::uuid)
		AND ($3::stock_transaction_type IS NULL OR t.type = $3::stock_transaction_type)`
	args := []any{garageID, nullable(filter.ItemID), nullable(string(filter.Type))}

	var total int
	if err := q.QueryRow(ctx, `SELECT count(*)`+transactionFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, `SELECT `+transactionColumns+transactionFrom+where+`
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

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// containsPattern turns free text into an ILIKE "contains" pattern, or ""
// when there is nothing to match (the queries treat "" as "no filter").
func containsPattern(query string) string {
	if query == "" {
		return ""
	}

	return "%" + likeEscaper.Replace(query) + "%"
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
