package repository_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/repository"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

const (
	receiptID  = "receipt-1"
	supplierID = "supplier-1"
	issueID    = "issue-1"
	orderID    = "order-1"
)

func receiptHeader() []any {
	return []any{receiptID, garageID, "PN-1", "draft", supplierID, "2026-10-15", "DN-1", "notes", nil, nil, "user", "user", now, now}
}

func receiptLineRow() []any {
	return []any{"line-1", partID, "unit-1", "10", "240", supplierID, int64(150000)}
}

func newReceipt() *domain.GoodsReceipt {
	unit, supplier, by := "unit-1", supplierID, "user"

	return &domain.GoodsReceipt{
		ID: receiptID, GarageID: garageID, Number: "PN-1", Status: domain.GoodsReceiptDraft, DefaultSupplierID: &supplier,
		ReceiptDate: "2026-10-15", UpdatedBy: &by, CreatedBy: &by,
		Lines: []domain.GoodsReceiptLine{
			{ID: "line-1", PartID: partID, UnitID: &unit, EnteredQuantity: "10", Quantity: "240", SupplierID: &supplier, UnitCost: 150000},
			{ID: "line-2", PartID: partID, EnteredQuantity: "1", Quantity: "1"},
		},
	}
}

//nolint:funlen // one table of receipt queries
func TestPostgresRepository_GoodsReceipt(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("create inserts the header and every line", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO goods_receipts", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO goods_receipt_lines", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO goods_receipt_lines", dbtest.Tag(1), nil)

		receipt := newReceipt()
		require.NoError(t, repo.CreateGoodsReceipt(tx.Context(t.Context()), receipt))
		assert.Equal(t, now, receipt.CreatedAt)
	})

	t.Run("create failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO goods_receipts", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("INSERT INTO goods_receipts", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO goods_receipt_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.CreateGoodsReceipt(ctx, newReceipt()), errBoom)
		require.ErrorIs(t, repo.CreateGoodsReceipt(ctx, newReceipt()), errBoom)
	})

	t.Run("find returns the receipt with its lines", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FOR UPDATE", dbtest.Row(receiptHeader()...)).
			ExpectQuery("FROM goods_receipt_lines", dbtest.NewRows(receiptLineRow()), nil)

		got, err := repo.FindGoodsReceipt(tx.Context(t.Context()), garageID, receiptID, true)
		require.NoError(t, err)
		assert.Equal(t, "2026-10-15", got.ReceiptDate)
		assert.Equal(t, domain.GoodsReceiptDraft, got.Status)
		require.Len(t, got.Lines, 1)
		assert.Equal(t, int64(150000), got.Lines[0].UnitCost)
	})

	t.Run("find failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM goods_receipts r", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("FROM goods_receipts r", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("FROM goods_receipts r", dbtest.Row(receiptHeader()...)).
			ExpectQuery("FROM goods_receipt_lines", nil, errBoom).
			ExpectQueryRow("FROM goods_receipts r", dbtest.Row(receiptHeader()...)).
			ExpectQuery("FROM goods_receipt_lines", dbtest.NewRows(receiptLineRow()).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.FindGoodsReceipt(ctx, garageID, receiptID, false)
		require.ErrorIs(t, err, domain.ErrGoodsReceiptNotFound)

		for range 3 {
			_, err = repo.FindGoodsReceipt(ctx, garageID, receiptID, false)
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("save updates the header and replaces the lines", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE goods_receipts", dbtest.Row(now)).
			ExpectExec("DELETE FROM goods_receipt_lines", dbtest.Tag(2), nil).
			ExpectExec("INSERT INTO goods_receipt_lines", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO goods_receipt_lines", dbtest.Tag(1), nil)

		require.NoError(t, repo.SaveGoodsReceipt(tx.Context(t.Context()), newReceipt()))
	})

	t.Run("save failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE goods_receipts", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("UPDATE goods_receipts", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("UPDATE goods_receipts", dbtest.Row(now)).
			ExpectExec("DELETE FROM goods_receipt_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.SaveGoodsReceipt(ctx, newReceipt()), domain.ErrGoodsReceiptNotFound)
		require.ErrorIs(t, repo.SaveGoodsReceipt(ctx, newReceipt()), errBoom)
		require.ErrorIs(t, repo.SaveGoodsReceipt(ctx, newReceipt()), errBoom)
	})

	t.Run("list returns a page with totals", func(t *testing.T) {
		t.Parallel()

		row := append(receiptHeader(), int64(1_500_000))
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM goods_receipts r", dbtest.NewRows(row), nil).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(0))
		ctx := tx.Context(t.Context())

		items, total, err := repo.ListGoodsReceipts(ctx, garageID, usecase.GoodsReceiptFilter{Query: "pn", Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		assert.Equal(t, int64(1_500_000), items[0].TotalAmount)

		items, total, err = repo.ListGoodsReceipts(ctx, garageID, usecase.GoodsReceiptFilter{Limit: 20})
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, items)
	})

	t.Run("list failures", func(t *testing.T) {
		t.Parallel()

		row := append(receiptHeader(), int64(1))
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM goods_receipts r", nil, errBoom).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM goods_receipts r", dbtest.NewRows(row).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		for range 3 {
			_, _, err := repo.ListGoodsReceipts(ctx, garageID, usecase.GoodsReceiptFilter{Limit: 20})
			require.ErrorIs(t, err, errBoom)
		}
	})
}

func TestPostgresRepository_PartSuppliers(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	listed := []any{supplierID, "A-1", "6250", true}

	t.Run("list", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQuery("FROM part_suppliers", dbtest.NewRows(listed), nil)

		got, err := repo.PartSuppliers(tx.Context(t.Context()), garageID, partID)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "6250", *got[0].LastUnitCost)
		assert.True(t, got[0].IsDefault)
	})

	t.Run("list failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQuery("FROM part_suppliers", nil, errBoom).
			ExpectQuery("FROM part_suppliers", dbtest.NewRows(listed).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.PartSuppliers(ctx, garageID, partID)
		require.ErrorIs(t, err, errBoom)
		_, err = repo.PartSuppliers(ctx, garageID, partID)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("replace keeps the last cost of a supplier that stays", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQuery("FROM part_suppliers", dbtest.NewRows(listed), nil).
			ExpectExec("DELETE FROM part_suppliers", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO part_suppliers", dbtest.Tag(1), nil).WithArgs(func(args []any) {
			require.Len(t, args, 6)
			cost, ok := args[4].(*string)
			require.True(t, ok)
			assert.Equal(t, "6250", *cost)
		})

		require.NoError(t, repo.ReplacePartSuppliers(tx.Context(t.Context()), garageID, partID, []domain.PartSupplier{{SupplierID: supplierID}}))
	})

	t.Run("replace failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQuery("FROM part_suppliers", nil, errBoom).
			ExpectQuery("FROM part_suppliers", dbtest.NewRows(), nil).
			ExpectExec("DELETE FROM part_suppliers", dbtest.Tag(0), errBoom).
			ExpectQuery("FROM part_suppliers", dbtest.NewRows(), nil).
			ExpectExec("DELETE FROM part_suppliers", dbtest.Tag(0), nil).
			ExpectExec("INSERT INTO part_suppliers", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())
		list := []domain.PartSupplier{{SupplierID: supplierID}}

		for range 3 {
			require.ErrorIs(t, repo.ReplacePartSuppliers(ctx, garageID, partID, list), errBoom)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQuery("is_default AND part_id", dbtest.NewRows([]any{partID, supplierID}), nil).
			ExpectQuery("is_default AND part_id", nil, errBoom).
			ExpectQuery("is_default AND part_id", dbtest.NewRows([]any{partID, supplierID}).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		got, err := repo.DefaultSuppliers(ctx, garageID, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{partID: supplierID}, got)

		_, err = repo.DefaultSuppliers(ctx, garageID, []string{partID})
		require.ErrorIs(t, err, errBoom)
		_, err = repo.DefaultSuppliers(ctx, garageID, []string{partID})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("last cost and usage", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectExec("INSERT INTO part_suppliers", dbtest.Tag(1), nil).
			ExpectQueryRow("FROM goods_receipt_lines", dbtest.Row(true))
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.RecordLastCost(ctx, garageID, partID, supplierID, "100"))

		used, err := repo.SupplierReferenced(ctx, garageID, supplierID)
		require.NoError(t, err)
		assert.True(t, used)
	})
}

func issueHeader() []any {
	return []any{issueID, garageID, "PX-1", orderID, "LSC-1", "draft", nil, nil, nil, nil, now, now}
}

func TestPostgresRepository_StockIssue(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	newIssue := func() *domain.StockIssue {
		return &domain.StockIssue{
			ID: issueID, GarageID: garageID, Number: "PX-1", RepairOrderID: orderID, Status: domain.StockIssueDraft,
			Lines: []domain.StockIssueLine{{ID: "l1", RepairOrderLineID: "rl1", PartID: partID, EnteredQuantity: "2", Quantity: "2"}},
		}
	}

	t.Run("create inserts the issue and its lines, once per order", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_issues", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO stock_issue_lines", dbtest.Tag(1), nil).
			ExpectQueryRow("INSERT INTO stock_issues", dbtest.ErrRow(pgx.ErrNoRows))
		ctx := tx.Context(t.Context())

		created, err := repo.CreateStockIssue(ctx, newIssue())
		require.NoError(t, err)
		assert.True(t, created)

		created, err = repo.CreateStockIssue(ctx, newIssue())
		require.NoError(t, err)
		assert.False(t, created)
	})

	t.Run("create failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO stock_issues", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("INSERT INTO stock_issues", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO stock_issue_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		for range 2 {
			_, err := repo.CreateStockIssue(ctx, newIssue())
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("find by id and by repair order", func(t *testing.T) {
		t.Parallel()

		line := []any{"l1", "rl1", partID, nil, "2", "2", "100", "0.5"}
		tx := dbtest.New(t).
			ExpectQueryRow("i.id = $2::uuid", dbtest.Row(issueHeader()...)).
			ExpectQuery("FROM stock_issue_lines", dbtest.NewRows(line), nil).
			ExpectQueryRow("i.repair_order_id = $2::uuid FOR UPDATE", dbtest.Row(issueHeader()...)).
			ExpectQuery("FROM stock_issue_lines", dbtest.NewRows(), nil)
		ctx := tx.Context(t.Context())

		got, err := repo.FindStockIssue(ctx, garageID, issueID, false)
		require.NoError(t, err)
		assert.Equal(t, domain.StockIssueDraft, got.Status)
		require.Len(t, got.Lines, 1)
		assert.Equal(t, "100", *got.Lines[0].UnitCost)
		assert.Equal(t, "0.5", got.Lines[0].ReturnedQuantity)

		got, err = repo.FindStockIssueByRepairOrder(ctx, garageID, orderID, true)
		require.NoError(t, err)
		assert.Empty(t, got.Lines)
	})

	t.Run("find failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM stock_issues i", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("FROM stock_issues i", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("FROM stock_issues i", dbtest.Row(issueHeader()...)).
			ExpectQuery("FROM stock_issue_lines", nil, errBoom).
			ExpectQueryRow("FROM stock_issues i", dbtest.Row(issueHeader()...)).
			ExpectQuery("FROM stock_issue_lines", dbtest.NewRows([]any{"l1", "rl1", partID, nil, "2", "2", nil}).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.FindStockIssue(ctx, garageID, issueID, false)
		require.ErrorIs(t, err, domain.ErrStockIssueNotFound)

		for range 3 {
			_, err = repo.FindStockIssue(ctx, garageID, issueID, false)
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("save", func(t *testing.T) {
		t.Parallel()

		posted := time.Now()
		issue := newIssue()
		issue.Status, issue.PostedAt = domain.StockIssuePosted, &posted

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE stock_issues", dbtest.Row(now)).
			ExpectExec("UPDATE stock_issue_lines", dbtest.Tag(1), nil).
			ExpectQueryRow("UPDATE stock_issues", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("UPDATE stock_issues", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("UPDATE stock_issues", dbtest.Row(now)).
			ExpectExec("UPDATE stock_issue_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.SaveStockIssue(ctx, issue))
		require.ErrorIs(t, repo.SaveStockIssue(ctx, issue), domain.ErrStockIssueNotFound)
		require.ErrorIs(t, repo.SaveStockIssue(ctx, issue), errBoom)
		require.ErrorIs(t, repo.SaveStockIssue(ctx, issue), errBoom)
	})
}

func TestPostgresRepository_Reports(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	purchase := []any{receiptID, "PN-1", "2026-10-10", supplierID, partID, "240", int64(260), int64(62500)}
	valuation := []any{partID, "200", "260.4167"}

	t.Run("purchases", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQuery("FROM goods_receipt_lines l", dbtest.NewRows(purchase), nil).
			ExpectQuery("FROM goods_receipt_lines l", nil, errBoom).
			ExpectQuery("FROM goods_receipt_lines l", dbtest.NewRows(purchase).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		got, err := repo.PurchasesReport(ctx, garageID, usecase.PurchaseFilter{SupplierID: supplierID, From: "2026-10-01"})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(62500), got[0].Amount)

		for range 2 {
			_, err = repo.PurchasesReport(ctx, garageID, usecase.PurchaseFilter{})
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("valuation", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQuery("DISTINCT ON", dbtest.NewRows(valuation), nil).
			ExpectQuery("DISTINCT ON", nil, errBoom).
			ExpectQuery("DISTINCT ON", dbtest.NewRows(valuation).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		got, err := repo.StockValuation(ctx, garageID, now)
		require.NoError(t, err)
		assert.Equal(t, []usecase.ValuationRow{{PartID: partID, Quantity: "200", AverageCost: "260.4167"}}, got)

		for range 2 {
			_, err = repo.StockValuation(ctx, garageID, now)
			require.ErrorIs(t, err, errBoom)
		}
	})
}
