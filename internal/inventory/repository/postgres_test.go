package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/repository"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

var (
	now     = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

const (
	garageID = "g-1"
	itemID   = "item-1"
)

func newItem() *domain.Item {
	return &domain.Item{ID: itemID, GarageID: garageID, SKU: "OIL-5W30", Name: "Engine oil 5W-30", Unit: "L", ReorderThreshold: 5}
}

func itemRow() []any {
	return []any{itemID, garageID, "OIL-5W30", "Engine oil 5W-30", "L", 5, nil, nil, now, now, "10"}
}

func TestPostgresRepository_CreateItem(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		item := newItem()

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO items", dbtest.Row(now, now))
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.CreateItem(ctx, item))
		require.Equal(t, now, item.CreatedAt)
	})

	t.Run("duplicate SKU", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO items", dbtest.ErrRow(dbtest.UniqueViolation("idx_items_garage_sku_unique")))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.CreateItem(ctx, newItem()), domain.ErrSKUTaken)
	})

	t.Run("unexpected insert error", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO items", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.CreateItem(ctx, newItem()), errBoom)
	})
}

func TestPostgresRepository_FindItemByID(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("FROM items i", dbtest.Row(itemRow()...))
		ctx := tx.Context(t.Context())

		item, err := repo.FindItemByID(ctx, garageID, itemID)
		require.NoError(t, err)
		require.Equal(t, "OIL-5W30", item.SKU)
		require.Equal(t, "10", item.QuantityOnHand)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("FROM items i", dbtest.ErrRow(pgx.ErrNoRows))
		ctx := tx.Context(t.Context())

		_, err := repo.FindItemByID(ctx, garageID, "missing")
		require.ErrorIs(t, err, domain.ErrItemNotFound)
	})
}

func TestPostgresRepository_UpdateItem(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("UPDATE items SET sku", dbtest.Row(now))
		ctx := tx.Context(t.Context())

		item := newItem()
		require.NoError(t, repo.UpdateItem(ctx, item))
		require.Equal(t, now, item.UpdatedAt)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("UPDATE items SET sku", dbtest.ErrRow(pgx.ErrNoRows))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.UpdateItem(ctx, newItem()), domain.ErrItemNotFound)
	})

	t.Run("duplicate SKU", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE items SET sku", dbtest.ErrRow(dbtest.UniqueViolation("idx_items_garage_sku_unique")))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.UpdateItem(ctx, newItem()), domain.ErrSKUTaken)
	})
}

func TestPostgresRepository_ListItems(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM items i", dbtest.NewRows(itemRow()), nil)
		ctx := tx.Context(t.Context())

		items, total, err := repo.ListItems(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, items, 1)
	})

	t.Run("no matches", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.Row(0))
		ctx := tx.Context(t.Context())

		items, total, err := repo.ListItems(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 0, total)
		require.Empty(t, items)
	})

	t.Run("count failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		_, _, err := repo.ListItems(ctx, garageID, usecase.ListFilter{})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("list query failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM items i", nil, errBoom)
		ctx := tx.Context(t.Context())

		_, _, err := repo.ListItems(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_SoftDeleteItem(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectExec("UPDATE items SET deleted_at", dbtest.Tag(1), nil).
		ExpectExec("UPDATE items SET deleted_at", dbtest.Tag(0), nil).
		ExpectExec("UPDATE items SET deleted_at", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.SoftDeleteItem(ctx, garageID, itemID, "actor-1"))
	require.ErrorIs(t, repo.SoftDeleteItem(ctx, garageID, "missing", "actor-1"), domain.ErrItemNotFound)
	require.ErrorIs(t, repo.SoftDeleteItem(ctx, garageID, itemID, "actor-1"), errBoom)
}

func TestPostgresRepository_RecordTransaction(t *testing.T) {
	t.Parallel()

	newTx := func() *domain.StockTransaction {
		return &domain.StockTransaction{
			ID: "tx-1", GarageID: garageID, ItemID: itemID, Type: domain.TransactionIn, Quantity: "5",
			RefType: domain.RefPurchase,
		}
	}

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectExec("INSERT INTO stock_transactions", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO stock_balance", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.RecordTransaction(ctx, newTx()))
	})

	t.Run("already issued", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectExec("INSERT INTO stock_transactions", dbtest.Tag(0), dbtest.UniqueViolation("idx_stock_transactions_repair_order_item_unique"))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.RecordTransaction(ctx, newTx()), domain.ErrAlreadyIssued)
	})

	t.Run("insert failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectExec("INSERT INTO stock_transactions", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.RecordTransaction(ctx, newTx()), errBoom)
	})

	t.Run("balance update failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectExec("INSERT INTO stock_transactions", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO stock_balance", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.RecordTransaction(ctx, newTx()), errBoom)
	})
}

func transactionRow() []any {
	return []any{"tx-1", itemID, "OIL-5W30", "Engine oil 5W-30", "in", "5", nil, "purchase", nil, nil, now}
}

func TestPostgresRepository_ListTransactions(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM stock_transactions t", dbtest.NewRows(transactionRow()), nil)
		ctx := tx.Context(t.Context())

		items, total, err := repo.ListTransactions(ctx, garageID, usecase.TransactionFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, items, 1)
		require.Equal(t, domain.TransactionIn, items[0].Type)
	})

	t.Run("no matches", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.Row(0))
		ctx := tx.Context(t.Context())

		items, total, err := repo.ListTransactions(ctx, garageID, usecase.TransactionFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 0, total)
		require.Empty(t, items)
	})

	t.Run("count failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		_, _, err := repo.ListTransactions(ctx, garageID, usecase.TransactionFilter{})
		require.ErrorIs(t, err, errBoom)
	})
}
