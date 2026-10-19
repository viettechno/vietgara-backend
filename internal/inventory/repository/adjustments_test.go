package repository_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/repository"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

const (
	countID    = "count-1"
	writeOffID = "writeoff-1"
	returnID   = "return-1"
)

func countHeader(status string) []any {
	return []any{countID, garageID, "KK-1", "Shelf A", "notes", status, nil, nil, nil, "user", "user", now, now}
}

func countLineRow() []any {
	return []any{"line-1", 0, partID, "unit-1", "2", "24", "20", "50", now}
}

func newCount() *domain.StockCount {
	by := "user"

	return &domain.StockCount{
		ID: countID, GarageID: garageID, Number: "KK-1", Name: "Shelf A", Status: domain.StockCountDraft, CreatedBy: &by, UpdatedBy: &by,
		Lines: []domain.StockCountLine{
			{ID: "line-1", PartID: partID, SystemQuantity: "20", Counted: new("24"), EnteredCounted: new("24"), AverageCost: new("50"), CountedAt: &now},
			{ID: "line-2", Position: 1, PartID: "part-2", SystemQuantity: "1"},
		},
	}
}

//nolint:funlen // one table of stock count queries
func TestPostgresRepository_StockCount(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("create inserts the header and the lines", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_counts", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO stock_count_lines", dbtest.Tag(2), nil)

		c := newCount()
		require.NoError(t, repo.CreateStockCount(tx.Context(t.Context()), c))
		assert.Equal(t, now, c.CreatedAt)
	})

	t.Run("create failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_counts", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("INSERT INTO stock_counts", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO stock_count_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.CreateStockCount(ctx, newCount()), errBoom)
		require.ErrorIs(t, repo.CreateStockCount(ctx, newCount()), errBoom)
	})

	t.Run("find returns the count with its lines", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FOR UPDATE", dbtest.Row(countHeader("draft")...)).
			ExpectQuery("FROM stock_count_lines l", dbtest.NewRows(countLineRow()), nil)

		got, err := repo.FindStockCount(tx.Context(t.Context()), garageID, countID, true)
		require.NoError(t, err)
		assert.Equal(t, domain.StockCountDraft, got.Status)
		require.Len(t, got.Lines, 1)
		assert.Equal(t, "24", *got.Lines[0].Counted)
		assert.Equal(t, "50", *got.Lines[0].AverageCost)
	})

	t.Run("find failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM stock_counts c", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("FROM stock_counts c", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("FROM stock_counts c", dbtest.Row(countHeader("draft")...)).
			ExpectQuery("FROM stock_count_lines l", nil, errBoom).
			ExpectQueryRow("FROM stock_counts c", dbtest.Row(countHeader("draft")...)).
			ExpectQuery("FROM stock_count_lines l", dbtest.NewRows(countLineRow()).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.FindStockCount(ctx, garageID, countID, false)
		require.ErrorIs(t, err, domain.ErrStockCountNotFound)

		for range 3 {
			_, err = repo.FindStockCount(ctx, garageID, countID, false)
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("save persists the header", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE stock_counts", dbtest.Row(now)).
			ExpectQueryRow("UPDATE stock_counts", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("UPDATE stock_counts", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.SaveStockCount(ctx, newCount()))
		require.ErrorIs(t, repo.SaveStockCount(ctx, newCount()), domain.ErrStockCountNotFound)
		require.ErrorIs(t, repo.SaveStockCount(ctx, newCount()), errBoom)
	})

	t.Run("line writes", func(t *testing.T) {
		t.Parallel()

		line := newCount().Lines[0]
		tx := dbtest.New(t).
			ExpectExec("INSERT INTO stock_count_lines", dbtest.Tag(1), nil).
			ExpectExec("UPDATE stock_count_lines", dbtest.Tag(1), nil).
			ExpectExec("UPDATE stock_count_lines", dbtest.Tag(0), nil).
			ExpectExec("UPDATE stock_count_lines", dbtest.Tag(0), errBoom).
			ExpectExec("DELETE FROM stock_count_lines", dbtest.Tag(1), nil).
			ExpectExec("DELETE FROM stock_count_lines", dbtest.Tag(0), nil).
			ExpectExec("DELETE FROM stock_count_lines", dbtest.Tag(0), errBoom).
			ExpectExec("DELETE FROM stock_count_lines", dbtest.Tag(3), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.InsertStockCountLines(ctx, garageID, countID, new("user"), []domain.StockCountLine{line}))
		require.NoError(t, repo.SaveStockCountLine(ctx, garageID, countID, &line))
		require.ErrorIs(t, repo.SaveStockCountLine(ctx, garageID, countID, &line), domain.ErrStockCountLineNotFound)
		require.ErrorIs(t, repo.SaveStockCountLine(ctx, garageID, countID, &line), errBoom)
		require.NoError(t, repo.DeleteStockCountLine(ctx, garageID, countID, "line-1"))
		require.ErrorIs(t, repo.DeleteStockCountLine(ctx, garageID, countID, "line-1"), domain.ErrStockCountLineNotFound)
		require.ErrorIs(t, repo.DeleteStockCountLine(ctx, garageID, countID, "line-1"), errBoom)
		require.NoError(t, repo.ClearStockCountLines(ctx, garageID, countID))
	})

	t.Run("list carries the tally and the value of the difference", func(t *testing.T) {
		t.Parallel()

		row := append(countHeader("completed"), 1, 2, 3, 4, int64(-500))
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM stock_counts c", dbtest.NewRows(row), nil).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(0)).
			ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM stock_counts c", nil, errBoom).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM stock_counts c", dbtest.NewRows(row).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		items, total, err := repo.ListStockCounts(ctx, garageID, usecase.StockCountFilter{Query: "kk", Status: domain.StockCountCompleted, Limit: 20})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Equal(t, domain.Tally{Uncounted: 1, Matched: 2, Over: 3, Short: 4}, items[0].Tally)
		assert.EqualValues(t, -500, items[0].TotalDifferenceValue)

		items, total, err = repo.ListStockCounts(ctx, garageID, usecase.StockCountFilter{Limit: 20})
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, items)

		for range 3 {
			_, _, err = repo.ListStockCounts(ctx, garageID, usecase.StockCountFilter{Limit: 20})
			require.ErrorIs(t, err, errBoom)
		}
	})
}

func newWriteOff() *domain.WriteOff {
	by := "user"

	return &domain.WriteOff{
		ID: writeOffID, GarageID: garageID, Number: "XH-1", Status: domain.AdjustmentCompleted, Reason: domain.WriteOffDamaged, CreatedBy: &by,
		UpdatedBy: &by, Lines: []domain.AdjustmentLine{
			{ID: "l1", PartID: partID, EnteredQuantity: "1", Quantity: "1", AverageCost: new("50")},
			{ID: "l2", Position: 1, PartID: partID, UnitID: new("unit-1"), EnteredQuantity: "1", Quantity: "12"},
		},
	}
}

func writeOffHeader() []any {
	return []any{writeOffID, garageID, "XH-1", "completed", "damaged", "notes", nil, "user", "user", now, now}
}

func adjustmentLineRow(credit int64) []any {
	return []any{"l1", 0, partID, nil, "1", "1", "50", credit}
}

func TestPostgresRepository_WriteOff(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("create inserts the header and every line", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO write_offs", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO write_off_lines", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO write_off_lines", dbtest.Tag(1), nil).
			ExpectQueryRow("INSERT INTO write_offs", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("INSERT INTO write_offs", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO write_off_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		w := newWriteOff()
		require.NoError(t, repo.CreateWriteOff(ctx, w))
		assert.Equal(t, now, w.UpdatedAt)
		require.ErrorIs(t, repo.CreateWriteOff(ctx, newWriteOff()), errBoom)
		require.ErrorIs(t, repo.CreateWriteOff(ctx, newWriteOff()), errBoom)
	})

	t.Run("find returns the write-off with its lines", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FOR UPDATE", dbtest.Row(writeOffHeader()...)).
			ExpectQuery("FROM write_off_lines", dbtest.NewRows(adjustmentLineRow(0)), nil).
			ExpectQueryRow("FROM write_offs w", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("FROM write_offs w", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("FROM write_offs w", dbtest.Row(writeOffHeader()...)).
			ExpectQuery("FROM write_off_lines", nil, errBoom).
			ExpectQueryRow("FROM write_offs w", dbtest.Row(writeOffHeader()...)).
			ExpectQuery("FROM write_off_lines", dbtest.NewRows(adjustmentLineRow(0)).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		got, err := repo.FindWriteOff(ctx, garageID, writeOffID, true)
		require.NoError(t, err)
		assert.Equal(t, domain.WriteOffDamaged, got.Reason)
		require.Len(t, got.Lines, 1)
		assert.Equal(t, "50", *got.Lines[0].AverageCost)

		_, err = repo.FindWriteOff(ctx, garageID, writeOffID, false)
		require.ErrorIs(t, err, domain.ErrWriteOffNotFound)

		for range 3 {
			_, err = repo.FindWriteOff(ctx, garageID, writeOffID, false)
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("save and list", func(t *testing.T) {
		t.Parallel()

		row := append(writeOffHeader(), int64(150))
		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE write_offs", dbtest.Row(now)).
			ExpectQueryRow("UPDATE write_offs", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("UPDATE write_offs", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM write_offs w", dbtest.NewRows(row), nil).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(0)).
			ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM write_offs w", nil, errBoom).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM write_offs w", dbtest.NewRows(row).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.SaveWriteOff(ctx, newWriteOff()))
		require.ErrorIs(t, repo.SaveWriteOff(ctx, newWriteOff()), domain.ErrWriteOffNotFound)
		require.ErrorIs(t, repo.SaveWriteOff(ctx, newWriteOff()), errBoom)

		items, total, err := repo.ListWriteOffs(ctx, garageID, usecase.WriteOffFilter{Reason: domain.WriteOffDamaged, Limit: 20})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.EqualValues(t, 150, items[0].TotalValue)

		items, _, err = repo.ListWriteOffs(ctx, garageID, usecase.WriteOffFilter{Limit: 20})
		require.NoError(t, err)
		assert.Empty(t, items)

		for range 3 {
			_, _, err = repo.ListWriteOffs(ctx, garageID, usecase.WriteOffFilter{Limit: 20})
			require.ErrorIs(t, err, errBoom)
		}
	})
}

func newSupplierReturn() *domain.SupplierReturn {
	by, receipt := "user", receiptID

	return &domain.SupplierReturn{
		ID: returnID, GarageID: garageID, Number: "TN-1", Status: domain.AdjustmentCompleted, SupplierID: supplierID, GoodsReceiptID: &receipt,
		CreatedBy: &by, UpdatedBy: &by, Lines: []domain.AdjustmentLine{
			{ID: "l1", PartID: partID, EnteredQuantity: "1", Quantity: "1", AverageCost: new("50"), CreditUnitCost: 70},
		},
	}
}

func supplierReturnHeader() []any {
	return []any{returnID, garageID, "TN-1", "completed", supplierID, receiptID, "CN-1", "notes", nil, "user", "user", now, now}
}

func TestPostgresRepository_SupplierReturn(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("create, find and save", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO supplier_returns", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO supplier_return_lines", dbtest.Tag(1), nil).
			ExpectQueryRow("INSERT INTO supplier_returns", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("INSERT INTO supplier_returns", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO supplier_return_lines", dbtest.Tag(0), errBoom).
			ExpectQueryRow("FOR UPDATE", dbtest.Row(supplierReturnHeader()...)).
			ExpectQuery("FROM supplier_return_lines", dbtest.NewRows(adjustmentLineRow(70)), nil).
			ExpectQueryRow("FROM supplier_returns s", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("FROM supplier_returns s", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("FROM supplier_returns s", dbtest.Row(supplierReturnHeader()...)).
			ExpectQuery("FROM supplier_return_lines", nil, errBoom).
			ExpectQueryRow("UPDATE supplier_returns", dbtest.Row(now)).
			ExpectQueryRow("UPDATE supplier_returns", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("UPDATE supplier_returns", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.CreateSupplierReturn(ctx, newSupplierReturn()))
		require.ErrorIs(t, repo.CreateSupplierReturn(ctx, newSupplierReturn()), errBoom)
		require.ErrorIs(t, repo.CreateSupplierReturn(ctx, newSupplierReturn()), errBoom)

		got, err := repo.FindSupplierReturn(ctx, garageID, returnID, true)
		require.NoError(t, err)
		assert.Equal(t, "CN-1", got.CreditNoteNumber)
		assert.Equal(t, receiptID, *got.GoodsReceiptID)
		assert.EqualValues(t, 70, got.Lines[0].CreditUnitCost)

		_, err = repo.FindSupplierReturn(ctx, garageID, returnID, false)
		require.ErrorIs(t, err, domain.ErrSupplierReturnNotFound)
		_, err = repo.FindSupplierReturn(ctx, garageID, returnID, false)
		require.ErrorIs(t, err, errBoom)
		_, err = repo.FindSupplierReturn(ctx, garageID, returnID, false)
		require.ErrorIs(t, err, errBoom)

		require.NoError(t, repo.SaveSupplierReturn(ctx, newSupplierReturn()))
		require.ErrorIs(t, repo.SaveSupplierReturn(ctx, newSupplierReturn()), domain.ErrSupplierReturnNotFound)
		require.ErrorIs(t, repo.SaveSupplierReturn(ctx, newSupplierReturn()), errBoom)
	})

	t.Run("list and receipt numbers", func(t *testing.T) {
		t.Parallel()

		row := append(supplierReturnHeader(), int64(70))
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM supplier_returns s", dbtest.NewRows(row), nil).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(0)).
			ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM supplier_returns s", nil, errBoom).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM supplier_returns s", dbtest.NewRows(row).WithScanError(errBoom), nil).
			ExpectQuery("FROM goods_receipts", dbtest.NewRows([]any{receiptID, "PN-1"}), nil).
			ExpectQuery("FROM goods_receipts", nil, errBoom).
			ExpectQuery("FROM goods_receipts", dbtest.NewRows([]any{receiptID, "PN-1"}).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		items, total, err := repo.ListSupplierReturns(ctx, garageID, usecase.SupplierReturnFilter{SupplierID: supplierID, Limit: 20})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.EqualValues(t, 70, items[0].CreditAmount)

		items, _, err = repo.ListSupplierReturns(ctx, garageID, usecase.SupplierReturnFilter{Limit: 20})
		require.NoError(t, err)
		assert.Empty(t, items)

		for range 3 {
			_, _, err = repo.ListSupplierReturns(ctx, garageID, usecase.SupplierReturnFilter{Limit: 20})
			require.ErrorIs(t, err, errBoom)
		}

		numbers, err := repo.GoodsReceiptNumbers(ctx, garageID, []string{receiptID})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{receiptID: "PN-1"}, numbers)

		for range 2 {
			_, err = repo.GoodsReceiptNumbers(ctx, garageID, []string{receiptID})
			require.ErrorIs(t, err, errBoom)
		}
	})
}

func TestPostgresRepository_PartReturn(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	by := "user"
	ret := &domain.RepairOrderPartReturn{
		ID: returnID, GarageID: garageID, RepairOrderID: orderID, StockIssueID: issueID, StockIssueLineID: "il1", RepairOrderLineID: "rl1",
		PartID: partID, EnteredQuantity: "1", Quantity: "1", Reason: "unused", UnitCost: new("50"), CreatedBy: &by,
	}
	row := []any{returnID, garageID, orderID, "LSC-1", issueID, "il1", "rl1", partID, nil, "1", "1", "unused", "50", "user", now}

	tx := dbtest.New(t).
		ExpectQueryRow("INSERT INTO repair_order_part_returns", dbtest.Row(now)).
		ExpectQuery("FROM repair_order_part_returns", dbtest.NewRows(row), nil).
		ExpectQuery("FROM repair_order_part_returns", nil, errBoom).
		ExpectQuery("FROM repair_order_part_returns", dbtest.NewRows(row).WithScanError(errBoom), nil)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.CreatePartReturn(ctx, ret))
	assert.Equal(t, now, ret.CreatedAt)

	list, err := repo.ListPartReturns(ctx, garageID, orderID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "unused", list[0].Reason)
	assert.Equal(t, "50", *list[0].UnitCost)

	for range 2 {
		_, err = repo.ListPartReturns(ctx, garageID, orderID)
		require.ErrorIs(t, err, errBoom)
	}
}

func TestPostgresRepository_LockBalancesAndStamping(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("lock balances", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectExec("FOR UPDATE", dbtest.Tag(2), nil).
			ExpectExec("FOR UPDATE", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.LockBalances(ctx, garageID, nil), "nothing to lock")
		require.NoError(t, repo.LockBalances(ctx, garageID, []string{partID, "part-2"}))
		require.ErrorIs(t, repo.LockBalances(ctx, garageID, []string{partID}), errBoom)
	})

	t.Run("an adjustment in is valued at the average cost without moving it", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_balance", dbtest.Row("12", "50")).
			ExpectQueryRow("INSERT INTO stock_transactions", dbtest.Row(now))
		ctx := tx.Context(t.Context())

		in := &domain.StockTransaction{
			ID: "t1", GarageID: garageID, PartID: partID, Type: domain.TransactionIn, Quantity: "2", RefType: domain.RefStockCount,
		}
		require.NoError(t, repo.RecordTransaction(ctx, in))
		require.NotNil(t, in.UnitCost)
		assert.Equal(t, "50", *in.UnitCost)
		assert.Equal(t, "12", in.QuantityAfter)
	})
}
