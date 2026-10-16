package repository_test

import (
	"errors"
	"testing"
	"time"

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
	partID   = "part-1"
)

func TestPostgresRepository_Balances(t *testing.T) {
	t.Parallel()

	t.Run("by ids", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQuery("FROM stock_balance", dbtest.NewRows([]any{partID, "12.5"}), nil)

		got, err := repo.Balances(tx.Context(t.Context()), garageID, []string{partID})
		require.NoError(t, err)
		require.Equal(t, map[string]string{partID: "12.5"}, got)
	})

	t.Run("every part", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQuery("FROM stock_balance", dbtest.NewRows(), nil)

		got, err := repo.Balances(tx.Context(t.Context()), garageID, nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQuery("FROM stock_balance", nil, errBoom).
			ExpectQuery("FROM stock_balance", dbtest.NewRows([]any{partID, "1"}).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.Balances(ctx, garageID, nil)
		require.ErrorIs(t, err, errBoom)

		_, err = repo.Balances(ctx, garageID, nil)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_RecordTransaction(t *testing.T) {
	t.Parallel()

	newTx := func() *domain.StockTransaction {
		unit, entered, cost := "unit-1", "2", "5000"

		return &domain.StockTransaction{
			ID: "tx-1", GarageID: garageID, PartID: partID, Type: domain.TransactionIn, Quantity: "48",
			UnitID: &unit, EnteredQuantity: &entered, UnitCost: &cost, RefType: domain.RefPurchase,
		}
	}

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.Row(now)).
			ExpectExec("INSERT INTO stock_balance", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		in := newTx()
		require.NoError(t, repo.RecordTransaction(ctx, in))
		require.Equal(t, now, in.CreatedAt)
	})

	t.Run("an issue subtracts", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.Row(now)).
			ExpectExec("quantity_on_hand - ", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		out := newTx()
		out.Type = domain.TransactionOut

		require.NoError(t, repo.RecordTransaction(ctx, out))
	})

	t.Run("already issued", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.ErrRow(dbtest.UniqueViolation("idx_stock_transactions_repair_order_item_unique")))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.RecordTransaction(ctx, newTx()), domain.ErrAlreadyIssued)
	})

	t.Run("insert failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO stock_transactions", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.RecordTransaction(ctx, newTx()), errBoom)
	})

	t.Run("balance update failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.Row(now)).
			ExpectExec("INSERT INTO stock_balance", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.RecordTransaction(ctx, newTx()), errBoom)
	})
}

func transactionRow() []any {
	return []any{"tx-1", partID, "in", "48", "5000", "unit-1", "2", "purchase", nil, nil, now}
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

		items, total, err := repo.ListTransactions(ctx, garageID, usecase.TransactionFilter{Limit: 20, PartID: partID, Type: domain.TransactionIn})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, items, 1)
		require.Equal(t, domain.TransactionIn, items[0].Type)
		require.Equal(t, "5000", *items[0].UnitCost)
		require.Equal(t, "2", *items[0].EnteredQuantity)
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

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM stock_transactions t", nil, errBoom).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM stock_transactions t", dbtest.NewRows(transactionRow()).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		for range 3 {
			_, _, err := repo.ListTransactions(ctx, garageID, usecase.TransactionFilter{})
			require.ErrorIs(t, err, errBoom)
		}
	})
}

func TestPostgresRepository_MovementsReference(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectQueryRow("FROM stock_transactions", dbtest.Row(true)).
		ExpectQueryRow("FROM stock_transactions", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	used, err := repo.MovementsReference(ctx, garageID, partID, "")
	require.NoError(t, err)
	require.True(t, used)

	_, err = repo.MovementsReference(ctx, garageID, "", "unit-1")
	require.ErrorIs(t, err, errBoom)
}
