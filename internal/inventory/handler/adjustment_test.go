package handler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

const (
	writeOffID = "d0d0d0d0-0000-0000-0000-000000000001"
	returnID   = "e0e0e0e0-0000-0000-0000-000000000001"
	roLineID   = "88888888-8888-8888-8888-888888888888"
)

func storedWriteOff() *domain.WriteOff {
	at := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)

	return &domain.WriteOff{
		ID: writeOffID, GarageID: garageID, Number: "XH-1", Status: domain.AdjustmentCompleted, Reason: domain.WriteOffExpired, Notes: "old",
		CreatedBy: new(actorID), CreatedAt: at, UpdatedAt: at, CancelledAt: &at,
		Lines: []domain.AdjustmentLine{{ID: "l1", PartID: partID, UnitID: new(unitID), EnteredQuantity: "1", Quantity: "12", AverageCost: new("50")}},
	}
}

func TestWriteOffHandlers(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.expectCountNames()

	e.repo.EXPECT().FindWriteOff(anyArg, garageID, writeOffID, false).Return(storedWriteOff(), nil).Twice()
	resp, err := e.srv.GetWriteOff(signedInWithCost(t), &inventoryv1.GetWriteOffRequest{GarageId: garageID, WriteOffId: writeOffID})
	require.NoError(t, err)

	w := resp.GetWriteOff()
	assert.Equal(t, inventoryv1.WriteOffReason_WRITE_OFF_REASON_EXPIRED, w.GetReason())
	assert.Equal(t, inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_COMPLETED, w.GetStatus())
	assert.Equal(t, int64(600), w.GetTotalValue())
	assert.Equal(t, int64(50), w.GetLines()[0].GetUnitCost())
	assert.Equal(t, int64(600), w.GetLines()[0].GetLineValue())
	assert.Equal(t, "ctn", w.GetLines()[0].GetUnitAbbreviation())
	assert.Equal(t, "Lan Nguyen", w.GetCreatedByName())
	assert.NotNil(t, w.GetCancelledAt())

	resp, err = e.srv.GetWriteOff(signedIn(t), &inventoryv1.GetWriteOffRequest{GarageId: garageID, WriteOffId: writeOffID})
	require.NoError(t, err)
	assert.Nil(t, resp.GetWriteOff().TotalValue)
	assert.Nil(t, resp.GetWriteOff().GetLines()[0].UnitCost)
	assert.Nil(t, resp.GetWriteOff().GetLines()[0].LineValue)

	e.repo.EXPECT().FindWriteOff(anyArg, garageID, writeOffID, false).Return(nil, domain.ErrWriteOffNotFound).Once()
	_, err = e.srv.GetWriteOff(signedIn(t), &inventoryv1.GetWriteOffRequest{GarageId: garageID, WriteOffId: writeOffID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	// List.
	listed := storedWriteOff()
	listed.Lines, listed.TotalValue = nil, 77
	e.repo.EXPECT().ListWriteOffs(anyArg, garageID, usecase.WriteOffFilter{
		Query: "x", Reason: domain.WriteOffDamaged, Status: domain.AdjustmentCancelled, Limit: 20,
	}).Return([]*domain.WriteOff{listed}, 1, nil).Once()
	list, err := e.srv.ListWriteOffs(signedInWithCost(t), &inventoryv1.ListWriteOffsRequest{
		GarageId: garageID, Query: "x", Reason: inventoryv1.WriteOffReason_WRITE_OFF_REASON_DAMAGED,
		Status: inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_CANCELLED,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(77), list.GetData()[0].GetTotalValue())

	e.repo.EXPECT().ListWriteOffs(anyArg, garageID, usecase.WriteOffFilter{Limit: 20}).Return(nil, 0, errBoom).Once()
	_, err = e.srv.ListWriteOffs(signedIn(t), &inventoryv1.ListWriteOffsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))

	// Create and cancel.
	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{Timezone: "Asia/Ho_Chi_Minh", WriteOffNumberTemplate: "XH-{SEQ:4}"}, nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "write_off", "XH-{SEQ:4}").Return(1, nil).Once()
	e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, "", "2").Return(usecase.ResolvedQuantity{Base: "2"}, nil).Once()
	e.repo.EXPECT().LockBalances(anyArg, garageID, anyArg).Return(nil).Once()
	e.repo.EXPECT().Balances(anyArg, garageID, anyArg).Return(map[string]usecase.Balance{partID: {OnHand: "5"}}, nil).Once()
	e.repo.EXPECT().RecordTransaction(anyArg, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
		tx.UnitCost = new("50")

		return tx.Reason == "lost"
	})).Return(nil).Once()
	e.repo.EXPECT().CreateWriteOff(anyArg, anyArg).Return(nil).Once()
	e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Twice()

	created, err := e.srv.CreateWriteOff(signedInWithCost(t), &inventoryv1.CreateWriteOffRequest{
		GarageId: garageID, WriteOff: &inventoryv1.WriteOff{
			Reason: inventoryv1.WriteOffReason_WRITE_OFF_REASON_LOST, Notes: "gone",
			Lines: []*inventoryv1.WriteOffLine{{PartId: partID, EnteredQuantity: "2"}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(100), created.GetWriteOff().GetTotalValue())

	_, err = e.srv.CreateWriteOff(signedIn(t), &inventoryv1.CreateWriteOffRequest{GarageId: garageID, WriteOff: &inventoryv1.WriteOff{}})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "a reason is required")

	e.repo.EXPECT().FindWriteOff(anyArg, garageID, writeOffID, true).Return(storedWriteOff(), nil).Once()
	e.repo.EXPECT().RecordTransaction(anyArg, anyArg).Return(nil).Once()
	e.repo.EXPECT().SaveWriteOff(anyArg, anyArg).Return(nil).Once()
	cancelled, err := e.srv.CancelWriteOff(signedIn(t), &inventoryv1.CancelWriteOffRequest{GarageId: garageID, WriteOffId: writeOffID})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_CANCELLED, cancelled.GetWriteOff().GetStatus())

	e.repo.EXPECT().FindWriteOff(anyArg, garageID, writeOffID, true).Return(nil, errBoom).Once()
	_, err = e.srv.CancelWriteOff(signedIn(t), &inventoryv1.CancelWriteOffRequest{GarageId: garageID, WriteOffId: writeOffID})
	assert.Equal(t, codes.Internal, status.Code(err))
}

func storedSupplierReturn() *domain.SupplierReturn {
	at := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)

	return &domain.SupplierReturn{
		ID: returnID, GarageID: garageID, Number: "TN-1", Status: domain.AdjustmentCompleted, SupplierID: supplierID,
		GoodsReceiptID: new(receiptID), CreditNoteNumber: "CN-1", Notes: "faulty", CreatedBy: new(actorID), CreatedAt: at, UpdatedAt: at,
		Lines: []domain.AdjustmentLine{{ID: "l1", PartID: partID, EnteredQuantity: "2", Quantity: "2", AverageCost: new("50"), CreditUnitCost: 70}},
	}
}

func TestSupplierReturnHandlers(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.expectCountNames()
	e.repo.EXPECT().GoodsReceiptNumbers(anyArg, garageID, anyArg).Return(map[string]string{receiptID: "PN-1"}, nil).Maybe()

	e.repo.EXPECT().FindSupplierReturn(anyArg, garageID, returnID, false).Return(storedSupplierReturn(), nil).Twice()
	resp, err := e.srv.GetSupplierReturn(signedInWithCost(t), &inventoryv1.GetSupplierReturnRequest{GarageId: garageID, SupplierReturnId: returnID})
	require.NoError(t, err)

	r := resp.GetSupplierReturn()
	assert.Equal(t, "Phu Tung Sai Gon", r.GetSupplierName())
	assert.Equal(t, "PN-1", r.GetGoodsReceiptNumber())
	assert.Equal(t, receiptID, r.GetGoodsReceiptId())
	assert.Equal(t, int64(140), r.GetCreditAmount())
	assert.Equal(t, int64(70), r.GetLines()[0].GetUnitCost())
	assert.Equal(t, int64(140), r.GetLines()[0].GetLineAmount())

	resp, err = e.srv.GetSupplierReturn(signedIn(t), &inventoryv1.GetSupplierReturnRequest{GarageId: garageID, SupplierReturnId: returnID})
	require.NoError(t, err)
	assert.Nil(t, resp.GetSupplierReturn().CreditAmount)
	assert.Nil(t, resp.GetSupplierReturn().GetLines()[0].UnitCost)

	e.repo.EXPECT().FindSupplierReturn(anyArg, garageID, returnID, false).Return(nil, domain.ErrSupplierReturnNotFound).Once()
	_, err = e.srv.GetSupplierReturn(signedIn(t), &inventoryv1.GetSupplierReturnRequest{GarageId: garageID, SupplierReturnId: returnID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	e.repo.EXPECT().ListSupplierReturns(anyArg, garageID, usecase.SupplierReturnFilter{
		Query: "cn", SupplierID: supplierID, Status: domain.AdjustmentCompleted, Limit: 20,
	}).Return([]*domain.SupplierReturn{storedSupplierReturn()}, 1, nil).Once()
	list, err := e.srv.ListSupplierReturns(signedIn(t), &inventoryv1.ListSupplierReturnsRequest{
		GarageId: garageID, Query: "cn", SupplierId: supplierID, Status: inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_COMPLETED,
	})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)

	e.repo.EXPECT().ListSupplierReturns(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()
	_, err = e.srv.ListSupplierReturns(signedIn(t), &inventoryv1.ListSupplierReturnsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))

	// Create: a typed credit counts for a caller with cost access.
	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{Timezone: "Asia/Ho_Chi_Minh", SupplierReturnNumberTemplate: "TN-{SEQ:4}"}, nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "supplier_return", "TN-{SEQ:4}").Return(1, nil).Once()
	e.repo.EXPECT().FindGoodsReceipt(anyArg, garageID, receiptID, false).Return(&domain.GoodsReceipt{
		ID: receiptID, Status: domain.GoodsReceiptCompleted, Lines: []domain.GoodsReceiptLine{{PartID: partID, SupplierID: new(supplierID)}},
	}, nil).Once()
	e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, "", "2").Return(usecase.ResolvedQuantity{Base: "2"}, nil).Once()
	e.repo.EXPECT().LockBalances(anyArg, garageID, anyArg).Return(nil).Once()
	e.repo.EXPECT().Balances(anyArg, garageID, anyArg).Return(map[string]usecase.Balance{partID: {OnHand: "5", AverageCost: "50"}}, nil).Once()
	e.repo.EXPECT().RecordTransaction(anyArg, anyArg).Return(nil).Once()
	e.repo.EXPECT().CreateSupplierReturn(anyArg, mock.MatchedBy(func(s *domain.SupplierReturn) bool {
		return s.Lines[0].CreditUnitCost == 90 && s.CreditNoteNumber == "CN-9"
	})).Return(nil).Once()
	e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Twice()

	created, err := e.srv.CreateSupplierReturn(signedInWithCost(t), &inventoryv1.CreateSupplierReturnRequest{
		GarageId: garageID, SupplierReturn: &inventoryv1.SupplierReturn{
			SupplierId: supplierID, GoodsReceiptId: new(receiptID), CreditNoteNumber: "CN-9",
			Lines: []*inventoryv1.SupplierReturnLine{{PartId: partID, EnteredQuantity: "2", UnitCost: new(int64(90))}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(180), created.GetSupplierReturn().GetCreditAmount())

	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{}, nil).Once()
	_, err = e.srv.CreateSupplierReturn(signedIn(t), &inventoryv1.CreateSupplierReturnRequest{GarageId: garageID, SupplierReturn: &inventoryv1.SupplierReturn{}})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	e.repo.EXPECT().FindSupplierReturn(anyArg, garageID, returnID, true).Return(storedSupplierReturn(), nil).Once()
	e.repo.EXPECT().RecordTransaction(anyArg, anyArg).Return(nil).Once()
	e.repo.EXPECT().SaveSupplierReturn(anyArg, anyArg).Return(nil).Once()
	cancelled, err := e.srv.CancelSupplierReturn(signedIn(t), &inventoryv1.CancelSupplierReturnRequest{GarageId: garageID, SupplierReturnId: returnID})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_CANCELLED, cancelled.GetSupplierReturn().GetStatus())

	e.repo.EXPECT().FindSupplierReturn(anyArg, garageID, returnID, true).Return(nil, errBoom).Once()
	_, err = e.srv.CancelSupplierReturn(signedIn(t), &inventoryv1.CancelSupplierReturnRequest{GarageId: garageID, SupplierReturnId: returnID})
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestPartReturnHandlers(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.expectCountNames()

	posted := &domain.StockIssue{
		ID: issueID, GarageID: garageID, RepairOrderID: orderID, RepairOrderNumber: "LSC-1", Status: domain.StockIssuePosted,
		Lines: []domain.StockIssueLine{{ID: "il1", RepairOrderLineID: roLineID, PartID: partID, Quantity: "4", UnitCost: new("50"), ReturnedQuantity: "1"}},
	}

	e.repo.EXPECT().FindStockIssueByRepairOrder(anyArg, garageID, orderID, true).Return(posted, nil).Once()
	e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, "", "2").Return(usecase.ResolvedQuantity{Base: "2"}, nil).Once()
	e.repo.EXPECT().RecordTransaction(anyArg, anyArg).Return(nil).Once()
	e.repo.EXPECT().CreatePartReturn(anyArg, anyArg).Return(nil).Once()
	e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Once()

	req := &inventoryv1.ReturnRepairOrderPartRequest{
		GarageId: garageID, RepairOrderId: orderID,
		PartReturn: &inventoryv1.RepairOrderPartReturn{RepairOrderLineId: roLineID, EnteredQuantity: "2", Reason: "unused"},
	}

	resp, err := e.srv.ReturnRepairOrderPart(signedInWithCost(t), req)
	require.NoError(t, err)
	assert.Equal(t, "unused", resp.GetPartReturn().GetReason())
	assert.Equal(t, int64(50), resp.GetPartReturn().GetUnitCost())
	assert.Equal(t, "LSC-1", resp.GetPartReturn().GetRepairOrderNumber())

	e.repo.EXPECT().FindStockIssueByRepairOrder(anyArg, garageID, orderID, true).Return(posted, nil).Once()
	e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, "", "2").Return(usecase.ResolvedQuantity{Base: "2"}, nil).Once()
	e.repo.EXPECT().RecordTransaction(anyArg, anyArg).Return(nil).Once()
	e.repo.EXPECT().CreatePartReturn(anyArg, anyArg).Return(nil).Once()
	e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Once()

	resp, err = e.srv.ReturnRepairOrderPart(signedIn(t), req)
	require.NoError(t, err)
	assert.Nil(t, resp.GetPartReturn().UnitCost)

	req.PartReturn.Reason = ""
	_, err = e.srv.ReturnRepairOrderPart(signedIn(t), req)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	e.repo.EXPECT().ListPartReturns(anyArg, garageID, orderID).Return([]domain.RepairOrderPartReturn{
		{ID: returnID, RepairOrderID: orderID, PartID: partID, Reason: "unused", EnteredQuantity: "2", Quantity: "2", UnitCost: new("50")},
	}, nil).Once()
	list, err := e.srv.ListRepairOrderPartReturns(signedInWithCost(t), &inventoryv1.ListRepairOrderPartReturnsRequest{GarageId: garageID, RepairOrderId: orderID})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)
	assert.Equal(t, "BEER-330", list.GetData()[0].GetPartSku())

	e.repo.EXPECT().ListPartReturns(anyArg, garageID, orderID).Return(nil, errBoom).Once()
	_, err = e.srv.ListRepairOrderPartReturns(signedIn(t), &inventoryv1.ListRepairOrderPartReturnsRequest{GarageId: garageID, RepairOrderId: orderID})
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestMappings(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.expectCountNames()

	// Every reference type travels to the API and back as a filter.
	types := []struct {
		domain domain.RefType
		proto  inventoryv1.StockTransactionRefType
	}{
		{domain.RefPurchase, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_PURCHASE},
		{domain.RefRepairOrder, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_REPAIR_ORDER},
		{domain.RefGoodsReceipt, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_GOODS_RECEIPT},
		{domain.RefStockIssue, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_STOCK_ISSUE},
		{domain.RefStockCount, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_STOCK_COUNT},
		{domain.RefWriteOff, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_WRITE_OFF},
		{domain.RefSupplierReturn, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_SUPPLIER_RETURN},
		{domain.RefRepairOrderReturn, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_REPAIR_ORDER_RETURN},
	}

	for _, tc := range types {
		e.repo.EXPECT().ListTransactions(anyArg, garageID, usecase.TransactionFilter{RefType: tc.domain, Limit: 20}).
			Return([]*domain.StockTransaction{{
				ID: "t", PartID: partID, Type: domain.TransactionIn, Quantity: "1", RefType: tc.domain, RefID: new(returnID), RefNumber: "N-1",
				Reason: "why", QuantityAfter: "9", CreatedBy: new(actorID),
			}}, 1, nil).Once()

		resp, err := e.srv.ListStockTransactions(signedIn(t), &inventoryv1.ListStockTransactionsRequest{GarageId: garageID, RefType: tc.proto})
		require.NoError(t, err, tc.domain)

		got := resp.GetData()[0]
		assert.Equal(t, tc.proto, got.GetRefType())
		assert.Equal(t, "N-1", got.GetRefNumber())
		assert.Equal(t, "why", got.GetReason())
		assert.Equal(t, "9", got.GetQuantityAfter())
		assert.Equal(t, "Lan Nguyen", got.GetCreatedByName())
	}

	e.repo.EXPECT().ListTransactions(anyArg, garageID, usecase.TransactionFilter{Limit: 20}).Return(nil, 0, nil).Times(2)

	for _, unknown := range []inventoryv1.StockTransactionRefType{inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_UNSPECIFIED, 99} {
		_, err := e.srv.ListStockTransactions(signedIn(t), &inventoryv1.ListStockTransactionsRequest{GarageId: garageID, RefType: unknown})
		require.NoError(t, err)
	}

	// Statuses and reasons outside the enum map to nothing.
	e.repo.EXPECT().ListWriteOffs(anyArg, garageID, usecase.WriteOffFilter{Limit: 20}).Return(nil, 0, nil).Times(2)

	_, err := e.srv.ListWriteOffs(signedIn(t), &inventoryv1.ListWriteOffsRequest{GarageId: garageID, Reason: 99, Status: 99})
	require.NoError(t, err)
	_, err = e.srv.ListWriteOffs(signedIn(t), &inventoryv1.ListWriteOffsRequest{GarageId: garageID})
	require.NoError(t, err)

	// A stock issue line shows what was returned.
	e.repo.EXPECT().FindStockIssueByRepairOrder(anyArg, garageID, orderID, false).Return(&domain.StockIssue{
		ID: issueID, Status: domain.StockIssueCancelled, Lines: []domain.StockIssueLine{{ID: "il", PartID: partID, ReturnedQuantity: "1.5"}},
	}, nil).Once()

	issue, err := e.srv.GetRepairOrderStockIssue(signedIn(t), &inventoryv1.GetRepairOrderStockIssueRequest{GarageId: garageID, RepairOrderId: orderID})
	require.NoError(t, err)
	assert.Equal(t, "1.5", issue.GetStockIssue().GetLines()[0].GetReturnedQuantity())
}
