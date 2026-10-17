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
		tx := dbtest.New(t).ExpectQuery("FROM stock_balance", dbtest.NewRows([]any{partID, "12.5", "2", "100.5"}), nil)

		got, err := repo.Balances(tx.Context(t.Context()), garageID, []string{partID})
		require.NoError(t, err)
		require.Equal(t, map[string]usecase.Balance{partID: {OnHand: "12.5", Reserved: "2", AverageCost: "100.5"}}, got)
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
			ExpectQuery("FROM stock_balance", dbtest.NewRows([]any{partID, "1", "0", "0"}).WithScanError(errBoom), nil)
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

	t.Run("a receipt with a cost moves the average", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("average_cost = CASE", dbtest.Row("96", "5000")).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.Row(now))
		ctx := tx.Context(t.Context())

		in := newTx()
		require.NoError(t, repo.RecordTransaction(ctx, in))
		require.Equal(t, now, in.CreatedAt)
	})

	t.Run("an issue subtracts and is stamped with the average cost", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("quantity_on_hand - ", dbtest.Row("46", "5000")).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.Row(now))
		ctx := tx.Context(t.Context())

		out := newTx()
		out.Type, out.RefType, out.UnitCost = domain.TransactionOut, domain.RefStockIssue, nil

		require.NoError(t, repo.RecordTransaction(ctx, out))
		require.NotNil(t, out.UnitCost)
		require.Equal(t, "5000", *out.UnitCost)
	})

	t.Run("an issue before any cost is known stays unstamped", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("quantity_on_hand - ", dbtest.Row("-2", "0")).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.Row(now))
		ctx := tx.Context(t.Context())

		out := newTx()
		out.Type, out.RefType, out.UnitCost = domain.TransactionOut, domain.RefStockIssue, nil

		require.NoError(t, repo.RecordTransaction(ctx, out))
		require.Nil(t, out.UnitCost)
	})

	t.Run("the reversal of a receipt line unwinds its cost", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("greatest(round(", dbtest.Row("10", "100")).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.Row(now))
		ctx := tx.Context(t.Context())

		out := newTx()
		out.Type, out.RefType = domain.TransactionOut, domain.RefGoodsReceipt

		require.NoError(t, repo.RecordTransaction(ctx, out))
		require.Equal(t, "5000", *out.UnitCost, "the line's own cost is kept")
	})

	t.Run("balance update failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO stock_balance", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.RecordTransaction(ctx, newTx()), errBoom)
	})

	t.Run("insert failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_balance", dbtest.Row("1", "1")).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.RecordTransaction(ctx, newTx()), errBoom)
	})
}

func transactionRow() []any {
	return []any{"tx-1", partID, "in", "48", "5000", "unit-1", "2", "purchase", nil, nil, now, "", "10", "PN-1"}
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

		parts, total, err := repo.ListTransactions(ctx, garageID, usecase.TransactionFilter{Limit: 20, PartID: partID, Type: domain.TransactionIn})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, parts, 1)
		require.Equal(t, domain.TransactionIn, parts[0].Type)
		require.Equal(t, "5000", *parts[0].UnitCost)
		require.Equal(t, "2", *parts[0].EnteredQuantity)
		require.Equal(t, "PN-1", parts[0].RefNumber)
		require.Equal(t, "10", parts[0].QuantityAfter)
	})

	t.Run("no matches", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.Row(0))
		ctx := tx.Context(t.Context())

		parts, total, err := repo.ListTransactions(ctx, garageID, usecase.TransactionFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 0, total)
		require.Empty(t, parts)
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
