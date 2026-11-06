//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

func (f *fixture) order(number string, status domain.PurchaseOrderStatus, lines ...domain.PurchaseOrderLine) *domain.PurchaseOrder {
	return &domain.PurchaseOrder{
		ID: uuid.NewString(), GarageID: f.garage, Number: number, Status: status, SupplierID: f.supplier, OrderDate: "2026-10-15",
		ExpectedDate: "2026-10-20", Notes: "morning", Lines: lines,
	}
}

func line(partID, quantity, received string, cost int64) domain.PurchaseOrderLine {
	return domain.PurchaseOrderLine{
		ID: uuid.NewString(), PartID: partID, EnteredQuantity: quantity, Quantity: quantity, ReceivedQuantity: received, UnitCost: cost,
	}
}

func TestPurchaseOrders_Lifecycle(t *testing.T) {
	f := newFixture(t)

	f.in(f.garage, func(ctx context.Context) {
		o := f.order("DH-2026-00001", domain.PurchaseOrderDraft, line(f.part, "10", "", 1000), line(f.part2, "5", "", 0))
		require.NoError(t, f.repo.CreatePurchaseOrder(ctx, o))

		got, err := f.repo.FindPurchaseOrder(ctx, f.garage, o.ID, true)
		require.NoError(t, err)
		assert.Equal(t, domain.PurchaseOrderDraft, got.Status)
		assert.Equal(t, "2026-10-20", got.ExpectedDate)
		require.Len(t, got.Lines, 2)
		assert.Equal(t, "0", got.Lines[0].ReceivedQuantity)
		assert.Equal(t, int64(10_000), got.Total())

		// A part may be on an order once.
		dup := f.order("DH-2026-00002", domain.PurchaseOrderDraft, line(f.part, "1", "", 0), line(f.part, "2", "", 0))
		require.Error(t, f.repo.CreatePurchaseOrder(ctx, dup))
	})

	f.in(f.garage, func(ctx context.Context) {
		o := f.order("DH-2026-00003", domain.PurchaseOrderSent, line(f.part, "10", "4", 1000))
		o.ExpectedDate = ""
		require.NoError(t, f.repo.CreatePurchaseOrder(ctx, o))

		o.Status, o.Lines[0].ReceivedQuantity, o.SentTo = domain.PurchaseOrderPartiallyReceived, "7", "sao@mai.test"
		require.NoError(t, f.repo.SavePurchaseOrder(ctx, o))

		got, err := f.repo.FindPurchaseOrder(ctx, f.garage, o.ID, false)
		require.NoError(t, err)
		assert.Equal(t, domain.PurchaseOrderPartiallyReceived, got.Status)
		assert.Empty(t, got.ExpectedDate, "no expected date stays empty")
		assert.Equal(t, "7", got.Lines[0].ReceivedQuantity)
		assert.Equal(t, "sao@mai.test", got.SentTo)

		_, err = f.repo.FindPurchaseOrder(ctx, f.garage, uuid.NewString(), false)
		require.ErrorIs(t, err, domain.ErrPurchaseOrderNotFound)
	})
}

func TestPurchaseOrders_ListOnOrderAndOpenReceipt(t *testing.T) {
	f := newFixture(t)

	f.in(f.garage, func(ctx context.Context) {
		draft := f.order("DH-2026-00001", domain.PurchaseOrderDraft, line(f.part, "10", "", 1000))
		partial := f.order("DH-2026-00002", domain.PurchaseOrderPartiallyReceived, line(f.part, "10", "4", 0), line(f.part2, "3", "3", 0))
		partial.SupplierID = f.sup2
		cancelled := f.order("DH-2026-00003", domain.PurchaseOrderCancelled, line(f.part, "99", "", 0))

		for _, o := range []*domain.PurchaseOrder{draft, partial, cancelled} {
			require.NoError(t, f.repo.CreatePurchaseOrder(ctx, o))
		}

		items, total, err := f.repo.ListPurchaseOrders(ctx, f.garage, usecase.PurchaseOrderFilter{Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, 3, total)
		require.Len(t, items, 3)

		byNumber := map[string]*domain.PurchaseOrder{}
		for _, o := range items {
			byNumber[o.Number] = o
		}

		assert.Equal(t, int64(10_000), byNumber["DH-2026-00001"].TotalAmount)

		_, total, err = f.repo.ListPurchaseOrders(ctx, f.garage, usecase.PurchaseOrderFilter{SupplierID: f.sup2, Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, 1, total)

		_, total, err = f.repo.ListPurchaseOrders(ctx, f.garage, usecase.PurchaseOrderFilter{Status: domain.PurchaseOrderDraft, Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, 1, total)

		_, total, err = f.repo.ListPurchaseOrders(ctx, f.garage, usecase.PurchaseOrderFilter{Query: "supplier b", Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, 1, total, "the query matches the supplier's name")

		// The draft's 10 and the partial order's 6 outstanding count; a cancelled order and a fully received line do not.
		onOrder, err := f.repo.OnOrder(ctx, f.garage, []string{f.part, f.part2})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{f.part: "16"}, onOrder)

		open, err := f.repo.HasDraftReceipt(ctx, f.garage, draft.ID)
		require.NoError(t, err)
		assert.False(t, open)

		receipt := &domain.GoodsReceipt{
			ID: uuid.NewString(), GarageID: f.garage, Number: "PN-1", Status: domain.GoodsReceiptDraft, ReceiptDate: "2026-10-15",
			PurchaseOrderID: &draft.ID, Lines: []domain.GoodsReceiptLine{{
				ID: uuid.NewString(), PartID: f.part, EnteredQuantity: "10", Quantity: "10", SupplierID: &f.supplier,
			}},
		}
		require.NoError(t, f.repo.CreateGoodsReceipt(ctx, receipt))

		open, err = f.repo.HasDraftReceipt(ctx, f.garage, draft.ID)
		require.NoError(t, err)
		assert.True(t, open)

		found, err := f.repo.FindGoodsReceipt(ctx, f.garage, receipt.ID, false)
		require.NoError(t, err)
		assert.Equal(t, draft.ID, *found.PurchaseOrderID)
		assert.Equal(t, "DH-2026-00001", found.PurchaseOrderNumber)

		page, _, err := f.repo.ListGoodsReceipts(ctx, f.garage, usecase.GoodsReceiptFilter{Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, "DH-2026-00001", page[0].PurchaseOrderNumber)

		receipt.Status = domain.GoodsReceiptCompleted
		require.NoError(t, f.repo.SaveGoodsReceipt(ctx, receipt))

		open, err = f.repo.HasDraftReceipt(ctx, f.garage, draft.ID)
		require.NoError(t, err)
		assert.False(t, open, "a completed receipt is not open")
	})
}

func TestPurchaseOrders_AnotherGarageSeesNothing(t *testing.T) {
	f := newFixture(t)

	var id string

	f.in(f.garage, func(ctx context.Context) {
		o := f.order("DH-2026-00001", domain.PurchaseOrderDraft, line(f.part, "10", "", 0))
		require.NoError(t, f.repo.CreatePurchaseOrder(ctx, o))

		id = o.ID
	})

	f.in(f.other, func(ctx context.Context) {
		_, err := f.repo.FindPurchaseOrder(ctx, f.other, id, false)
		require.ErrorIs(t, err, domain.ErrPurchaseOrderNotFound)

		_, total, err := f.repo.ListPurchaseOrders(ctx, f.other, usecase.PurchaseOrderFilter{Limit: 10})
		require.NoError(t, err)
		assert.Zero(t, total)

		onOrder, err := f.repo.OnOrder(ctx, f.other, []string{f.part})
		require.NoError(t, err)
		assert.Empty(t, onOrder)
	})
}
