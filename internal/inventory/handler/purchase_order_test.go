package handler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/handler"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

func storedOrder(status domain.PurchaseOrderStatus) *domain.PurchaseOrder {
	at := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)

	return &domain.PurchaseOrder{
		ID: orderID, GarageID: garageID, Number: "DH-2026-00001", Status: status, SupplierID: supplierID, OrderDate: "2026-10-15",
		ExpectedDate: "2026-10-20", Notes: "morning", CreatedAt: at, UpdatedAt: at, SentAt: &at,
		Lines: []domain.PurchaseOrderLine{{
			ID: "ol-1", PartID: partID, UnitID: new(unitID), EnteredQuantity: "3", Quantity: "12", ReceivedQuantity: "4", UnitCost: 1000,
		}},
	}
}

func (e *env) expectOrderNames() {
	e.parts.EXPECT().Parts(anyArg, garageID, anyArg).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
	e.parts.EXPECT().UnitAbbreviations(anyArg, garageID, anyArg).Return(map[string]string{unitID: "ctn"}, nil).Maybe()
	e.directory.EXPECT().SupplierContacts(anyArg, garageID, anyArg).
		Return(map[string]usecase.SupplierContact{supplierID: {Name: "Sao Mai", Email: "sao@mai.test"}}, nil).Maybe()
}

func TestPurchaseOrderPolicies(t *testing.T) {
	t.Parallel()

	p := handler.PurchaseOrderPolicies()
	assert.Len(t, p, 8)
	assert.Equal(t, access.InventoryRead, p[inventoryv1.PurchaseOrderService_ListPurchaseOrders_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryRead, p[inventoryv1.PurchaseOrderService_GetPurchaseOrder_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryWrite, p[inventoryv1.PurchaseOrderService_SendPurchaseOrder_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryWrite, p[inventoryv1.PurchaseOrderService_ReceivePurchaseOrder_FullMethodName].Garage.Permission)
}

func TestPurchaseOrderServer_ReadsShowCostsOnlyToThoseWhoMaySee(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	srv := handler.NewPurchaseOrderServer(e.uc)
	e.expectOrderNames()
	e.repo.EXPECT().FindPurchaseOrder(anyArg, garageID, orderID, false).Return(storedOrder(domain.PurchaseOrderPartiallyReceived), nil).Twice()
	e.repo.EXPECT().ListPurchaseOrders(anyArg, garageID, usecase.PurchaseOrderFilter{
		Query: "dh", SupplierID: supplierID, Status: domain.PurchaseOrderSent, Limit: 20,
	}).Return([]*domain.PurchaseOrder{storedOrder(domain.PurchaseOrderSent)}, 1, nil).Once()

	got, err := srv.GetPurchaseOrder(signedInWithCost(t), &inventoryv1.GetPurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_PARTIALLY_RECEIVED, got.GetStatus())
	assert.Equal(t, "Sao Mai", got.GetSupplierName())
	assert.Equal(t, "sao@mai.test", got.GetSupplierEmail())
	assert.Equal(t, int64(3000), got.GetTotalAmount())
	assert.Equal(t, int64(3000), got.GetLines()[0].GetLineAmount())
	assert.Equal(t, "4", got.GetLines()[0].GetReceivedQuantity())
	assert.Equal(t, "ctn", got.GetLines()[0].GetUnitAbbreviation())

	got, err = srv.GetPurchaseOrder(signedIn(t), &inventoryv1.GetPurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID})
	require.NoError(t, err)
	assert.Nil(t, got.TotalAmount, "costs are omitted without inventory.cost.read")
	assert.Nil(t, got.GetLines()[0].UnitCost)
	assert.Nil(t, got.GetLines()[0].LineAmount)

	list, err := srv.ListPurchaseOrders(signedIn(t), &inventoryv1.ListPurchaseOrdersRequest{
		GarageId: garageID, Query: "dh", SupplierId: supplierID, Status: inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_SENT,
	})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)
	assert.EqualValues(t, 1, list.GetPagination().GetTotal())
}

func TestPurchaseOrderServer_NotFoundIsA404(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	srv := handler.NewPurchaseOrderServer(e.uc)
	e.repo.EXPECT().FindPurchaseOrder(anyArg, garageID, orderID, false).Return(nil, domain.ErrPurchaseOrderNotFound).Once()

	_, err := srv.GetPurchaseOrder(signedIn(t), &inventoryv1.GetPurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestPurchaseOrderServer_CreateAndUpdate(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	srv := handler.NewPurchaseOrderServer(e.uc)
	e.expectOrderNames()
	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{Timezone: "Asia/Ho_Chi_Minh"}, nil).Maybe()
	e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Maybe()
	e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, unitID, "3").
		Return(usecase.ResolvedQuantity{Base: "12", UnitID: new(unitID), Entered: new("3")}, nil).Maybe()
	e.sequences.EXPECT().Next(anyArg, garageID, "purchase_order", anyArg).Return(1, nil).Once()
	e.repo.EXPECT().CreatePurchaseOrder(anyArg, anyArg).Return(nil).Once()
	e.repo.EXPECT().FindPurchaseOrder(anyArg, garageID, orderID, true).Return(func() *domain.PurchaseOrder {
		o := storedOrder(domain.PurchaseOrderDraft)
		o.Lines[0].ReceivedQuantity = ""

		return o
	}(), nil).Once()
	e.repo.EXPECT().SavePurchaseOrder(anyArg, anyArg).Return(nil).Once()

	body := &inventoryv1.PurchaseOrder{
		SupplierId: supplierID, ExpectedDate: "2026-10-20", Notes: "morning",
		Lines: []*inventoryv1.PurchaseOrderLine{{PartId: partID, UnitId: new(unitID), EnteredQuantity: "3", UnitCost: new(int64(1000))}},
	}

	created, err := srv.CreatePurchaseOrder(signedInWithCost(t), &inventoryv1.CreatePurchaseOrderRequest{GarageId: garageID, PurchaseOrder: body})
	require.NoError(t, err)
	assert.Equal(t, "DH-2026-00001", created.GetNumber())
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_DRAFT, created.GetStatus())
	assert.Equal(t, int64(3000), created.GetTotalAmount())

	updated, err := srv.UpdatePurchaseOrder(signedInWithCost(t), &inventoryv1.UpdatePurchaseOrderRequest{
		GarageId: garageID, PurchaseOrderId: orderID, PurchaseOrder: body,
	})
	require.NoError(t, err)
	assert.Equal(t, "2026-10-20", updated.GetExpectedDate())

	_, err = srv.CreatePurchaseOrder(signedIn(t), &inventoryv1.CreatePurchaseOrderRequest{
		GarageId: garageID, PurchaseOrder: &inventoryv1.PurchaseOrder{SupplierId: supplierID},
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "an order without lines is refused")
}

func TestPurchaseOrderServer_FromLowStock(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	srv := handler.NewPurchaseOrderServer(e.uc)
	low := samplePart()
	low.ReorderThreshold = "10"
	e.parts.EXPECT().SearchParts(anyArg, garageID, anyArg).Return([]usecase.PartInfo{low}, 1, nil).Once()
	e.repo.EXPECT().Balances(anyArg, garageID, []string(nil)).Return(map[string]usecase.Balance{partID: {OnHand: "2"}}, nil).Once()
	e.repo.EXPECT().OnOrder(anyArg, garageID, anyArg).Return(nil, nil).Once()
	e.repo.EXPECT().DefaultSuppliers(anyArg, garageID, anyArg).Return(nil, nil).Once()

	got, err := srv.CreatePurchaseOrdersFromLowStock(signedIn(t), &inventoryv1.CreatePurchaseOrdersFromLowStockRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Empty(t, got.GetOrders())
	require.Len(t, got.GetUnassignedParts(), 1)
	assert.Equal(t, "BEER-330", got.GetUnassignedParts()[0].GetPartSku())
}

func TestPurchaseOrderServer_SendCancelReceive(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	srv := handler.NewPurchaseOrderServer(e.uc)
	mailer := mocks.NewMockOrderMailer(t)
	e.uc.SetOrderMailer(mailer)
	e.expectOrderNames()
	e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Maybe()
	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{Timezone: "Asia/Ho_Chi_Minh"}, nil).Maybe()

	// send
	e.repo.EXPECT().FindPurchaseOrder(anyArg, garageID, orderID, true).Return(storedOrder(domain.PurchaseOrderDraft), nil).Once()
	mailer.EXPECT().SendPurchaseOrder(anyArg, usecase.OrderMail{
		GarageID: garageID, OrderID: orderID, Number: "DH-2026-00001", SupplierName: "Sao Mai", To: "sao@mai.test", Message: "hi",
	}).Return(true, nil).Once()
	e.repo.EXPECT().SavePurchaseOrder(anyArg, anyArg).Return(nil).Twice()

	sent, err := srv.SendPurchaseOrder(signedIn(t), &inventoryv1.SendPurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID, Message: "hi"})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_SENT, sent.GetStatus())
	assert.Equal(t, "sao@mai.test", sent.GetSentTo())
	assert.NotNil(t, sent.GetSentAt())

	// cancel
	e.repo.EXPECT().FindPurchaseOrder(anyArg, garageID, orderID, true).Return(storedOrder(domain.PurchaseOrderSent), nil).Once()
	e.repo.EXPECT().HasDraftReceipt(anyArg, garageID, orderID).Return(false, nil).Once()

	cancelled, err := srv.CancelPurchaseOrder(signedIn(t), &inventoryv1.CancelPurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_CANCELLED, cancelled.GetStatus())

	// receive
	e.repo.EXPECT().FindPurchaseOrder(anyArg, garageID, orderID, true).Return(storedOrder(domain.PurchaseOrderSent), nil).Once()
	e.repo.EXPECT().HasDraftReceipt(anyArg, garageID, orderID).Return(true, nil).Once()

	_, err = srv.ReceivePurchaseOrder(signedIn(t), &inventoryv1.ReceivePurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestPurchaseOrderServer_ReceiveAnswersTheReceipt(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	srv := handler.NewPurchaseOrderServer(e.uc)
	e.expectOrderNames()
	e.directory.EXPECT().SupplierNames(anyArg, garageID, anyArg).Return(map[string]string{supplierID: "Sao Mai"}, nil).Maybe()
	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{Timezone: "Asia/Ho_Chi_Minh"}, nil).Once()
	e.repo.EXPECT().FindPurchaseOrder(anyArg, garageID, orderID, true).Return(storedOrder(domain.PurchaseOrderSent), nil).Once()
	e.repo.EXPECT().HasDraftReceipt(anyArg, garageID, orderID).Return(false, nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "goods_receipt", anyArg).Return(1, nil).Once()
	e.repo.EXPECT().DefaultSuppliers(anyArg, garageID, anyArg).Return(nil, nil).Once()
	e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, unitID, "2").
		Return(usecase.ResolvedQuantity{Base: "8", UnitID: new(unitID), Entered: new("2")}, nil).Once()
	e.repo.EXPECT().CreateGoodsReceipt(anyArg, anyArg).Return(nil).Once()

	receipt, err := srv.ReceivePurchaseOrder(signedInWithCost(t), &inventoryv1.ReceivePurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID})
	require.NoError(t, err)
	assert.Equal(t, orderID, receipt.GetPurchaseOrderId())
	assert.Equal(t, "DH-2026-00001", receipt.GetPurchaseOrderNumber())
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_DRAFT, receipt.GetStatus())
}

func TestPurchaseOrderServer_EveryRPCMapsItsErrors(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	srv := handler.NewPurchaseOrderServer(e.uc)
	ctx := signedIn(t)

	e.repo.EXPECT().ListPurchaseOrders(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()
	_, err := srv.ListPurchaseOrders(ctx, &inventoryv1.ListPurchaseOrdersRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))

	e.repo.EXPECT().FindPurchaseOrder(anyArg, garageID, orderID, true).Return(nil, domain.ErrPurchaseOrderNotFound).Times(3)

	_, err = srv.SendPurchaseOrder(ctx, &inventoryv1.SendPurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = srv.CancelPurchaseOrder(ctx, &inventoryv1.CancelPurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = srv.ReceivePurchaseOrder(ctx, &inventoryv1.ReceivePurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{}, errBoom).Maybe()
	e.repo.EXPECT().Balances(anyArg, garageID, []string(nil)).Return(nil, errBoom).Once()

	_, err = srv.CreatePurchaseOrdersFromLowStock(ctx, &inventoryv1.CreatePurchaseOrdersFromLowStockRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))

	_, err = srv.CreatePurchaseOrder(ctx, &inventoryv1.CreatePurchaseOrderRequest{GarageId: garageID, PurchaseOrder: &inventoryv1.PurchaseOrder{}})
	assert.Equal(t, codes.Internal, status.Code(err))

	e.repo.EXPECT().FindPurchaseOrder(anyArg, garageID, orderID, true).Return(nil, domain.ErrPurchaseOrderNotFound).Once()

	_, err = srv.UpdatePurchaseOrder(ctx, &inventoryv1.UpdatePurchaseOrderRequest{GarageId: garageID, PurchaseOrderId: orderID, PurchaseOrder: &inventoryv1.PurchaseOrder{}})
	assert.Equal(t, codes.NotFound, status.Code(err))
}
