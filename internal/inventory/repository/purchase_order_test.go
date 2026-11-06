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

func orderHeader() []any {
	return []any{orderID, garageID, "DH-2026-00001", "sent", supplierID, "2026-10-15", "2026-10-20", "notes", nil, "", nil, "user", "user", now, now}
}

func orderLineRow() []any {
	return []any{"ol-1", partID, "unit-1", "3", "12", "4", int64(1000)}
}

func newOrder() *domain.PurchaseOrder {
	unit, by := "unit-1", "user"

	return &domain.PurchaseOrder{
		ID: orderID, GarageID: garageID, Number: "DH-2026-00001", Status: domain.PurchaseOrderDraft, SupplierID: supplierID,
		OrderDate: "2026-10-15", ExpectedDate: "2026-10-20", UpdatedBy: &by, CreatedBy: &by,
		Lines: []domain.PurchaseOrderLine{
			{ID: "ol-1", PartID: partID, UnitID: &unit, EnteredQuantity: "3", Quantity: "12", ReceivedQuantity: "4", UnitCost: 1000},
			{ID: "ol-2", PartID: partID, EnteredQuantity: "1", Quantity: "1"},
		},
	}
}

//nolint:funlen // one table of purchase order queries
func TestPostgresRepository_PurchaseOrder(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("create inserts the header and every line", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO purchase_orders", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO purchase_order_lines", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO purchase_order_lines", dbtest.Tag(1), nil)

		o := newOrder()
		require.NoError(t, repo.CreatePurchaseOrder(tx.Context(t.Context()), o))
		assert.Equal(t, now, o.CreatedAt)
	})

	t.Run("create failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO purchase_orders", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("INSERT INTO purchase_orders", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO purchase_order_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.CreatePurchaseOrder(ctx, newOrder()), errBoom)
		require.ErrorIs(t, repo.CreatePurchaseOrder(ctx, newOrder()), errBoom)
	})

	t.Run("find returns the order with its lines", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FOR UPDATE", dbtest.Row(orderHeader()...)).
			ExpectQuery("FROM purchase_order_lines", dbtest.NewRows(orderLineRow()), nil)

		got, err := repo.FindPurchaseOrder(tx.Context(t.Context()), garageID, orderID, true)
		require.NoError(t, err)
		assert.Equal(t, domain.PurchaseOrderSent, got.Status)
		assert.Equal(t, "2026-10-20", got.ExpectedDate)
		require.Len(t, got.Lines, 1)
		assert.Equal(t, "4", got.Lines[0].ReceivedQuantity)
	})

	t.Run("find failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM purchase_orders o", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("FROM purchase_orders o", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("FROM purchase_orders o", dbtest.Row(orderHeader()...)).
			ExpectQuery("FROM purchase_order_lines", nil, errBoom).
			ExpectQueryRow("FROM purchase_orders o", dbtest.Row(orderHeader()...)).
			ExpectQuery("FROM purchase_order_lines", dbtest.NewRows(orderLineRow()).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.FindPurchaseOrder(ctx, garageID, orderID, false)
		require.ErrorIs(t, err, domain.ErrPurchaseOrderNotFound)

		for range 3 {
			_, err = repo.FindPurchaseOrder(ctx, garageID, orderID, false)
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("save updates the header and replaces the lines", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE purchase_orders", dbtest.Row(now)).
			ExpectExec("DELETE FROM purchase_order_lines", dbtest.Tag(2), nil).
			ExpectExec("INSERT INTO purchase_order_lines", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO purchase_order_lines", dbtest.Tag(1), nil)

		require.NoError(t, repo.SavePurchaseOrder(tx.Context(t.Context()), newOrder()))
	})

	t.Run("save failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE purchase_orders", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("UPDATE purchase_orders", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("UPDATE purchase_orders", dbtest.Row(now)).
			ExpectExec("DELETE FROM purchase_order_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.SavePurchaseOrder(ctx, newOrder()), domain.ErrPurchaseOrderNotFound)
		require.ErrorIs(t, repo.SavePurchaseOrder(ctx, newOrder()), errBoom)
		require.ErrorIs(t, repo.SavePurchaseOrder(ctx, newOrder()), errBoom)
	})

	t.Run("list returns a page with totals", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM purchase_orders o", dbtest.NewRows(append(orderHeader(), int64(12_000))), nil).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(0))
		ctx := tx.Context(t.Context())

		items, total, err := repo.ListPurchaseOrders(ctx, garageID, usecase.PurchaseOrderFilter{Query: "dh", Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		assert.Equal(t, int64(12_000), items[0].TotalAmount)

		items, total, err = repo.ListPurchaseOrders(ctx, garageID, usecase.PurchaseOrderFilter{Limit: 20})
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, items)
	})

	t.Run("list failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM purchase_orders o", nil, errBoom).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM purchase_orders o", dbtest.NewRows(append(orderHeader(), int64(1))).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		for range 3 {
			_, _, err := repo.ListPurchaseOrders(ctx, garageID, usecase.PurchaseOrderFilter{Limit: 20})
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("on order sums what is outstanding per part", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQuery("FROM purchase_order_lines l", dbtest.NewRows([]any{partID, "8"}), nil).
			ExpectQuery("FROM purchase_order_lines l", nil, errBoom).
			ExpectQuery("FROM purchase_order_lines l", dbtest.NewRows([]any{partID, "8"}).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		got, err := repo.OnOrder(ctx, garageID, []string{partID})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{partID: "8"}, got)

		for range 2 {
			_, err = repo.OnOrder(ctx, garageID, []string{partID})
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("has draft receipt", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM goods_receipts", dbtest.Row(true)).
			ExpectQueryRow("FROM goods_receipts", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		open, err := repo.HasDraftReceipt(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.True(t, open)

		_, err = repo.HasDraftReceipt(ctx, garageID, orderID)
		require.ErrorIs(t, err, errBoom)
	})
}
