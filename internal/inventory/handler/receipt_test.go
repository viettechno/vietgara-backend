package handler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

const (
	receiptID  = "44444444-4444-4444-4444-444444444444"
	supplierID = "55555555-5555-5555-5555-555555555555"
	issueID    = "66666666-6666-6666-6666-666666666666"
	orderID    = "77777777-7777-7777-7777-777777777777"
)

func storedReceipt() *domain.GoodsReceipt {
	done := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)

	return &domain.GoodsReceipt{
		ID: receiptID, GarageID: garageID, Number: "PN-202610-0001", Status: domain.GoodsReceiptCompleted, ReceiptDate: "2026-10-15",
		DefaultSupplierID: new(supplierID), CompletedAt: &done, CreatedAt: done, UpdatedAt: done,
		Lines: []domain.GoodsReceiptLine{{
			ID: "l1", PartID: partID, EnteredQuantity: "10", Quantity: "10", SupplierID: new(supplierID), UnitCost: 150000,
		}},
	}
}

func (e *env) expectNames() {
	e.parts.EXPECT().Parts(anyArg, garageID, anyArg).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
	e.directory.EXPECT().SupplierNames(anyArg, garageID, anyArg).Return(map[string]string{supplierID: "Phu Tung Sai Gon"}, nil).Maybe()
}

func TestGetGoodsReceipt(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, false).Return(storedReceipt(), nil).Twice()
	e.expectNames()

	resp, err := e.srv.GetGoodsReceipt(signedInWithCost(t), &inventoryv1.GetGoodsReceiptRequest{GarageId: garageID, GoodsReceiptId: receiptID})
	require.NoError(t, err)

	r := resp.GetGoodsReceipt()
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_COMPLETED, r.GetStatus())
	assert.Equal(t, "Phu Tung Sai Gon", r.GetDefaultSupplierName())
	assert.Equal(t, int64(1_500_000), r.GetTotalAmount())
	assert.Equal(t, int64(150000), r.GetLines()[0].GetUnitCost())
	assert.Equal(t, int64(1_500_000), r.GetLines()[0].GetLineAmount())
	assert.Equal(t, "BEER-330", r.GetLines()[0].GetPartSku())
	assert.NotNil(t, r.GetCompletedAt())

	resp, err = e.srv.GetGoodsReceipt(signedIn(t), &inventoryv1.GetGoodsReceiptRequest{GarageId: garageID, GoodsReceiptId: receiptID})
	require.NoError(t, err)
	assert.Nil(t, resp.GetGoodsReceipt().TotalAmount, "costs are omitted without inventory.cost.read")
	assert.Nil(t, resp.GetGoodsReceipt().GetLines()[0].UnitCost)
	assert.Nil(t, resp.GetGoodsReceipt().GetLines()[0].LineAmount)

	e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, false).Return(nil, domain.ErrGoodsReceiptNotFound).Once()
	_, err = e.srv.GetGoodsReceipt(signedIn(t), &inventoryv1.GetGoodsReceiptRequest{GarageId: garageID, GoodsReceiptId: receiptID})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestListGoodsReceipts(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	listed := storedReceipt()
	listed.Lines, listed.TotalAmount = nil, 99

	e.repo.EXPECT().ListGoodsReceipts(anyArg, garageID, usecase.GoodsReceiptFilter{
		Query: "pn", SupplierID: supplierID, Status: domain.GoodsReceiptCompleted, Limit: 10, Offset: 10,
	}).Return([]*domain.GoodsReceipt{listed}, 11, nil).Once()
	e.expectNames()

	resp, err := e.srv.ListGoodsReceipts(signedInWithCost(t), &inventoryv1.ListGoodsReceiptsRequest{
		GarageId: garageID, Query: "pn", SupplierId: supplierID,
		Status: inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_COMPLETED, Page: 2, PageSize: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(11), resp.GetPagination().GetTotal())
	assert.Equal(t, int64(99), resp.GetData()[0].GetTotalAmount())

	e.repo.EXPECT().ListGoodsReceipts(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()
	_, err = e.srv.ListGoodsReceipts(signedIn(t), &inventoryv1.ListGoodsReceiptsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestCreateGoodsReceipt(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{
		Timezone: "Asia/Ho_Chi_Minh", GoodsReceiptNumberTemplate: "PN-{YYYY}{MM}-{SEQ:4}",
	}, nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "goods_receipt", "PN-202610-{SEQ:4}").Return(1, nil).Once()
	e.parts.EXPECT().Parts(anyArg, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
	e.repo.EXPECT().DefaultSuppliers(anyArg, garageID, []string{partID}).Return(nil, nil).Once()
	e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, unitID, "2").
		Return(usecase.ResolvedQuantity{Base: "48", UnitID: new(unitID), Entered: new("2")}, nil).Once()
	e.directory.EXPECT().SupplierNames(anyArg, garageID, anyArg).Return(map[string]string{supplierID: "Phu Tung Sai Gon"}, nil).Twice()
	e.parts.EXPECT().UnitAbbreviations(anyArg, garageID, []string{unitID}).Return(map[string]string{unitID: "case"}, nil).Once()
	e.parts.EXPECT().Parts(anyArg, garageID, anyArg).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
	e.repo.EXPECT().CreateGoodsReceipt(anyArg, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
		return r.Lines[0].UnitCost == 120000 && *r.Lines[0].SupplierID == supplierID && r.ReceiptDate == "2026-10-15"
	})).Return(nil).Once()

	resp, err := e.srv.CreateGoodsReceipt(signedInWithCost(t), &inventoryv1.CreateGoodsReceiptRequest{
		GarageId: garageID,
		GoodsReceipt: &inventoryv1.GoodsReceipt{
			DefaultSupplierId: new(supplierID), DeliveryNoteNumber: "DN-1",
			Lines: []*inventoryv1.GoodsReceiptLine{{PartId: partID, UnitId: new(unitID), EnteredQuantity: "2", UnitCost: new(int64(120000))}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_DRAFT, resp.GetGoodsReceipt().GetStatus())
	assert.Equal(t, "48", resp.GetGoodsReceipt().GetLines()[0].GetQuantity())
	assert.Equal(t, "case", resp.GetGoodsReceipt().GetLines()[0].GetUnitAbbreviation())
	assert.Equal(t, int64(240000), resp.GetGoodsReceipt().GetTotalAmount())
}

func TestUpdateGoodsReceipt(t *testing.T) {
	t.Parallel()

	t.Run("applies the masked fields", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		draft := storedReceipt()
		draft.Status = domain.GoodsReceiptDraft
		e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, true).Return(draft, nil).Once()
		e.repo.EXPECT().SaveGoodsReceipt(anyArg, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return r.Notes == "changed" && r.DeliveryNoteNumber == "DN-2" && r.DefaultSupplierID == nil && r.ReceiptDate == "2026-10-01"
		})).Return(nil).Once()
		e.expectNames()

		_, err := e.srv.UpdateGoodsReceipt(signedIn(t), &inventoryv1.UpdateGoodsReceiptRequest{
			GarageId: garageID, GoodsReceiptId: receiptID,
			GoodsReceipt: &inventoryv1.GoodsReceipt{Notes: "changed", DeliveryNoteNumber: "DN-2", ReceiptDate: "2026-10-01"},
			UpdateMask:   &fieldmaskpb.FieldMask{Paths: []string{"notes", "delivery_note_number", "default_supplier_id", "receipt_date"}},
		})
		require.NoError(t, err)
	})

	t.Run("without a mask the fields that are set are applied", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		draft := storedReceipt()
		draft.Status = domain.GoodsReceiptDraft
		e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, true).Return(draft, nil).Once()
		e.repo.EXPECT().SaveGoodsReceipt(anyArg, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return r.Notes == "only this" && r.DeliveryNoteNumber == "" && *r.DefaultSupplierID == supplierID
		})).Return(nil).Once()
		e.expectNames()

		_, err := e.srv.UpdateGoodsReceipt(signedIn(t), &inventoryv1.UpdateGoodsReceiptRequest{
			GarageId: garageID, GoodsReceiptId: receiptID, GoodsReceipt: &inventoryv1.GoodsReceipt{Notes: "only this"},
		})
		require.NoError(t, err)
	})

	t.Run("replaces the lines", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		draft := storedReceipt()
		draft.Status = domain.GoodsReceiptDraft
		e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, true).Return(draft, nil).Once()
		e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, "", "3").Return(usecase.ResolvedQuantity{Base: "3"}, nil).Once()
		e.repo.EXPECT().DefaultSuppliers(anyArg, garageID, anyArg).Return(nil, nil).Once()
		e.repo.EXPECT().SaveGoodsReceipt(anyArg, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return len(r.Lines) == 1 && r.Lines[0].Quantity == "3" && r.Lines[0].UnitCost == 2000
		})).Return(nil).Once()
		e.expectNames()

		_, err := e.srv.UpdateGoodsReceipt(signedInWithCost(t), &inventoryv1.UpdateGoodsReceiptRequest{
			GarageId: garageID, GoodsReceiptId: receiptID,
			GoodsReceipt: &inventoryv1.GoodsReceipt{Lines: []*inventoryv1.GoodsReceiptLine{
				{Id: "l1", PartId: partID, EnteredQuantity: "3", UnitCost: new(int64(2000))},
			}},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"lines"}},
		})
		require.NoError(t, err)
	})

	t.Run("a path that cannot be updated is refused", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		_, err := e.srv.UpdateGoodsReceipt(signedIn(t), &inventoryv1.UpdateGoodsReceiptRequest{
			GarageId: garageID, GoodsReceiptId: receiptID, GoodsReceipt: &inventoryv1.GoodsReceipt{},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
		})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("a completed receipt is refused", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, true).Return(storedReceipt(), nil).Once()

		_, err := e.srv.UpdateGoodsReceipt(signedIn(t), &inventoryv1.UpdateGoodsReceiptRequest{
			GarageId: garageID, GoodsReceiptId: receiptID, GoodsReceipt: &inventoryv1.GoodsReceipt{Notes: "x"},
		})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	})
}

func TestCompleteAndCancelGoodsReceipt(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	newDraft := func() *domain.GoodsReceipt {
		draft := storedReceipt()
		draft.Status, draft.CompletedAt = domain.GoodsReceiptDraft, nil

		return draft
	}

	e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, true).Return(newDraft(), nil).Once()
	e.repo.EXPECT().RecordTransaction(anyArg, anyArg).Return(nil).Once()
	e.repo.EXPECT().RecordLastCost(anyArg, garageID, partID, supplierID, "150000").Return(nil).Once()
	e.repo.EXPECT().SaveGoodsReceipt(anyArg, anyArg).Return(nil).Once()
	e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Once()
	e.expectNames()

	resp, err := e.srv.CompleteGoodsReceipt(signedIn(t), &inventoryv1.CompleteGoodsReceiptRequest{GarageId: garageID, GoodsReceiptId: receiptID})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_COMPLETED, resp.GetGoodsReceipt().GetStatus())

	e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, true).Return(storedReceipt(), nil).Once()
	e.repo.EXPECT().Balances(anyArg, garageID, []string{partID}).Return(map[string]usecase.Balance{partID: {OnHand: "3"}}, nil).Once()

	_, err = e.srv.CancelGoodsReceipt(signedIn(t), &inventoryv1.CancelGoodsReceiptRequest{GarageId: garageID, GoodsReceiptId: receiptID})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err), "stock already used")

	e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, true).Return(newDraft(), nil).Once()
	e.repo.EXPECT().SaveGoodsReceipt(anyArg, anyArg).Return(nil).Once()
	e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Once()

	cancelled, err := e.srv.CancelGoodsReceipt(signedIn(t), &inventoryv1.CancelGoodsReceiptRequest{GarageId: garageID, GoodsReceiptId: receiptID})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_CANCELLED, cancelled.GetGoodsReceipt().GetStatus())
}

func TestStockIssue(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	posted := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)
	issue := &domain.StockIssue{
		ID: issueID, GarageID: garageID, Number: "PX-1", RepairOrderID: orderID, RepairOrderNumber: "LSC-1", Status: domain.StockIssuePosted,
		PostedAt: &posted, Lines: []domain.StockIssueLine{{ID: "l1", RepairOrderLineID: "ro-l1", PartID: partID, Quantity: "2", EnteredQuantity: "2", UnitCost: new("5000.4")}},
	}
	e.repo.EXPECT().FindStockIssueByRepairOrder(anyArg, garageID, orderID, false).Return(issue, nil).Twice()
	e.parts.EXPECT().Parts(anyArg, garageID, anyArg).Return([]usecase.PartInfo{samplePart()}, nil).Twice()

	resp, err := e.srv.GetRepairOrderStockIssue(signedInWithCost(t), &inventoryv1.GetRepairOrderStockIssueRequest{GarageId: garageID, RepairOrderId: orderID})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_POSTED, resp.GetStockIssue().GetStatus())
	assert.Equal(t, "BEER-330", resp.GetStockIssue().GetLines()[0].GetPartSku())
	assert.Equal(t, int64(5000), resp.GetStockIssue().GetLines()[0].GetUnitCost())

	resp, err = e.srv.GetRepairOrderStockIssue(signedIn(t), &inventoryv1.GetRepairOrderStockIssueRequest{GarageId: garageID, RepairOrderId: orderID})
	require.NoError(t, err)
	assert.Nil(t, resp.GetStockIssue().GetLines()[0].UnitCost, "no cost without inventory.cost.read")

	e.repo.EXPECT().FindStockIssue(anyArg, garageID, issueID, true).Return(issue, nil).Once()
	_, err = e.srv.PostStockIssue(signedIn(t), &inventoryv1.PostStockIssueRequest{GarageId: garageID, StockIssueId: issueID})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err), "already posted")
}

func TestPartSuppliers(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.parts.EXPECT().Parts(anyArg, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Times(3)
	e.repo.EXPECT().PartSuppliers(anyArg, garageID, partID).Return([]domain.PartSupplier{
		{SupplierID: supplierID, SupplierSKU: "A-1", IsDefault: true, LastUnitCost: new("6250.5")},
	}, nil).Times(3)
	e.directory.EXPECT().SupplierNames(anyArg, garageID, anyArg).Return(map[string]string{supplierID: "Phu Tung Sai Gon"}, nil).Times(4)

	resp, err := e.srv.GetPartSuppliers(signedInWithCost(t), &inventoryv1.GetPartSuppliersRequest{GarageId: garageID, PartId: partID})
	require.NoError(t, err)
	assert.Equal(t, int64(6251), resp.GetData()[0].GetLastUnitCost())
	assert.Equal(t, "Phu Tung Sai Gon", resp.GetData()[0].GetSupplierName())

	resp, err = e.srv.GetPartSuppliers(signedIn(t), &inventoryv1.GetPartSuppliersRequest{GarageId: garageID, PartId: partID})
	require.NoError(t, err)
	assert.Nil(t, resp.GetData()[0].LastUnitCost)

	e.repo.EXPECT().ReplacePartSuppliers(anyArg, garageID, partID, []domain.PartSupplier{
		{SupplierID: supplierID, SupplierSKU: "A-1", IsDefault: true, SupplierName: "Phu Tung Sai Gon"},
	}).Return(nil).Once()

	put, err := e.srv.PutPartSuppliers(signedInWithCost(t), &inventoryv1.PutPartSuppliersRequest{
		GarageId: garageID, PartId: partID,
		Data: []*inventoryv1.PartSupplier{{SupplierId: supplierID, SupplierSku: "A-1", IsDefault: true, LastUnitCost: new(int64(1))}},
	})
	require.NoError(t, err)
	assert.True(t, put.GetData()[0].GetIsDefault())
}

func TestReports(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().PurchasesReport(anyArg, garageID, usecase.PurchaseFilter{SupplierID: supplierID, From: "2026-10-01", To: "2026-11-01"}).
		Return([]domain.PurchaseLine{{GoodsReceiptNumber: "PN-1", PartID: partID, SupplierID: supplierID, Quantity: "10", UnitCost: 100, Amount: 1000}}, nil).Once()
	e.expectNames()

	purchases, err := e.srv.GetPurchasesReport(signedInWithCost(t), &inventoryv1.GetPurchasesReportRequest{
		GarageId: garageID, SupplierId: supplierID, From: "2026-10-01", To: "2026-11-01",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1000), purchases.GetTotalAmount())
	assert.Equal(t, "Phu Tung Sai Gon", purchases.GetData()[0].GetSupplierName())

	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{Timezone: "Asia/Ho_Chi_Minh"}, nil).Once()
	e.repo.EXPECT().StockValuation(anyArg, garageID, anyArg).
		Return([]usecase.ValuationRow{{PartID: partID, Quantity: "4", AverageCost: "250"}}, nil).Once()

	valuation, err := e.srv.GetStockValuationReport(signedInWithCost(t), &inventoryv1.GetStockValuationReportRequest{GarageId: garageID, At: "2026-10-15"})
	require.NoError(t, err)
	assert.Equal(t, int64(1000), valuation.GetTotalValue())
	assert.Equal(t, "2026-10-15", valuation.GetAt())
	assert.Len(t, valuation.GetCategories(), 1)

	_, err = e.srv.GetPurchasesReport(signedInWithCost(t), &inventoryv1.GetPurchasesReportRequest{GarageId: garageID, From: "soon"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
