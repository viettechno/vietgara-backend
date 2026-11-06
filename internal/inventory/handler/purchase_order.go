package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// PurchaseOrderServer implements inventoryv1.PurchaseOrderServiceServer
// (FR-INV-16).
type PurchaseOrderServer struct {
	inventoryv1.UnimplementedPurchaseOrderServiceServer

	uc *usecase.InventoryUseCase
}

// NewPurchaseOrderServer wires the handler to the Inventory use case.
func NewPurchaseOrderServer(uc *usecase.InventoryUseCase) *PurchaseOrderServer {
	return &PurchaseOrderServer{uc: uc}
}

// PurchaseOrderPolicies declares the permission every RPC requires: reading
// needs inventory.read, changing inventory.write.
func PurchaseOrderPolicies() middleware.Policies {
	read, write := access.Requires(access.InventoryRead), access.Requires(access.InventoryWrite)

	return middleware.Policies{
		inventoryv1.PurchaseOrderService_ListPurchaseOrders_FullMethodName:               middleware.GarageMethod(read),
		inventoryv1.PurchaseOrderService_GetPurchaseOrder_FullMethodName:                 middleware.GarageMethod(read),
		inventoryv1.PurchaseOrderService_CreatePurchaseOrder_FullMethodName:              middleware.GarageMethod(write),
		inventoryv1.PurchaseOrderService_UpdatePurchaseOrder_FullMethodName:              middleware.GarageMethod(write),
		inventoryv1.PurchaseOrderService_CreatePurchaseOrdersFromLowStock_FullMethodName: middleware.GarageMethod(write),
		inventoryv1.PurchaseOrderService_SendPurchaseOrder_FullMethodName:                middleware.GarageMethod(write),
		inventoryv1.PurchaseOrderService_CancelPurchaseOrder_FullMethodName:              middleware.GarageMethod(write),
		inventoryv1.PurchaseOrderService_ReceivePurchaseOrder_FullMethodName:             middleware.GarageMethod(write),
	}
}

// ListPurchaseOrders lists the garage's orders, newest first.
func (s *PurchaseOrderServer) ListPurchaseOrders(ctx context.Context, req *inventoryv1.ListPurchaseOrdersRequest) (*inventoryv1.ListPurchaseOrdersResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListPurchaseOrdersResponse, error) {
		page, err := s.uc.ListPurchaseOrders(ctx, usecase.ListPurchaseOrdersRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(), Query: req.GetQuery(),
			SupplierID: req.GetSupplierId(), Status: orderStatusFromProto(req.GetStatus()),
		})
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.ListPurchaseOrdersResponse{
			Data: make([]*inventoryv1.PurchaseOrder, 0, len(page.Items)), Pagination: paginationToProto(page),
		}
		for _, o := range page.Items {
			resp.Data = append(resp.Data, orderToProto(ctx, o))
		}

		return resp, nil
	})
}

// GetPurchaseOrder returns one order with its lines.
func (s *PurchaseOrderServer) GetPurchaseOrder(ctx context.Context, req *inventoryv1.GetPurchaseOrderRequest) (*inventoryv1.PurchaseOrder, error) {
	return run(ctx, func(string) (*inventoryv1.PurchaseOrder, error) {
		o, err := s.uc.GetPurchaseOrder(ctx, req.GetGarageId(), req.GetPurchaseOrderId())
		if err != nil {
			return nil, err
		}

		return orderToProto(ctx, o), nil
	})
}

// CreatePurchaseOrder saves a new draft order.
func (s *PurchaseOrderServer) CreatePurchaseOrder(ctx context.Context, req *inventoryv1.CreatePurchaseOrderRequest) (*inventoryv1.PurchaseOrder, error) {
	return run(ctx, func(actorID string) (*inventoryv1.PurchaseOrder, error) {
		in := req.GetPurchaseOrder()

		o, err := s.uc.CreatePurchaseOrder(ctx, usecase.CreatePurchaseOrderRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, CanSetCost: canSeeCost(ctx), SupplierID: in.GetSupplierId(),
			ExpectedDate: in.GetExpectedDate(), Notes: in.GetNotes(), Lines: orderLinesFromProto(in.GetLines()),
		})
		if err != nil {
			return nil, err
		}

		return orderToProto(ctx, o), nil
	})
}

// UpdatePurchaseOrder replaces a draft order.
func (s *PurchaseOrderServer) UpdatePurchaseOrder(ctx context.Context, req *inventoryv1.UpdatePurchaseOrderRequest) (*inventoryv1.PurchaseOrder, error) {
	return run(ctx, func(actorID string) (*inventoryv1.PurchaseOrder, error) {
		in := req.GetPurchaseOrder()

		o, err := s.uc.UpdatePurchaseOrder(ctx, usecase.UpdatePurchaseOrderRequest{
			GarageID: req.GetGarageId(), OrderID: req.GetPurchaseOrderId(), ActorID: actorID, CanSetCost: canSeeCost(ctx),
			SupplierID: in.GetSupplierId(), ExpectedDate: in.GetExpectedDate(), Notes: in.GetNotes(), Lines: orderLinesFromProto(in.GetLines()),
		})
		if err != nil {
			return nil, err
		}

		return orderToProto(ctx, o), nil
	})
}

// CreatePurchaseOrdersFromLowStock makes one draft per default supplier.
func (s *PurchaseOrderServer) CreatePurchaseOrdersFromLowStock(
	ctx context.Context, req *inventoryv1.CreatePurchaseOrdersFromLowStockRequest,
) (*inventoryv1.CreatePurchaseOrdersFromLowStockResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CreatePurchaseOrdersFromLowStockResponse, error) {
		res, err := s.uc.CreatePurchaseOrdersFromLowStock(ctx, usecase.CreatePurchaseOrdersFromLowStockRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, PartIDs: req.GetPartIds(), ExpectedDate: req.GetExpectedDate(),
		})
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.CreatePurchaseOrdersFromLowStockResponse{}
		for _, o := range res.Orders {
			resp.Orders = append(resp.Orders, orderToProto(ctx, o))
		}

		for _, l := range res.Unassigned {
			resp.UnassignedParts = append(resp.UnassignedParts, &inventoryv1.UnassignedPart{PartId: l.PartID, PartSku: l.SKU, PartName: l.Name})
		}

		return resp, nil
	})
}

// SendPurchaseOrder e-mails the order to the supplier's address on file.
func (s *PurchaseOrderServer) SendPurchaseOrder(ctx context.Context, req *inventoryv1.SendPurchaseOrderRequest) (*inventoryv1.PurchaseOrder, error) {
	return run(ctx, func(actorID string) (*inventoryv1.PurchaseOrder, error) {
		o, err := s.uc.SendPurchaseOrder(ctx, usecase.SendPurchaseOrderRequest{
			GarageID: req.GetGarageId(), OrderID: req.GetPurchaseOrderId(), ActorID: actorID, Message: req.GetMessage(),
		})
		if err != nil {
			return nil, err
		}

		return orderToProto(ctx, o), nil
	})
}

// CancelPurchaseOrder cancels an order.
func (s *PurchaseOrderServer) CancelPurchaseOrder(ctx context.Context, req *inventoryv1.CancelPurchaseOrderRequest) (*inventoryv1.PurchaseOrder, error) {
	return run(ctx, func(actorID string) (*inventoryv1.PurchaseOrder, error) {
		o, err := s.uc.CancelPurchaseOrder(ctx, req.GetGarageId(), req.GetPurchaseOrderId(), actorID)
		if err != nil {
			return nil, err
		}

		return orderToProto(ctx, o), nil
	})
}

// ReceivePurchaseOrder creates the draft goods receipt for what is outstanding.
func (s *PurchaseOrderServer) ReceivePurchaseOrder(ctx context.Context, req *inventoryv1.ReceivePurchaseOrderRequest) (*inventoryv1.GoodsReceipt, error) {
	return run(ctx, func(actorID string) (*inventoryv1.GoodsReceipt, error) {
		r, err := s.uc.ReceivePurchaseOrder(ctx, req.GetGarageId(), req.GetPurchaseOrderId(), actorID)
		if err != nil {
			return nil, err
		}

		return receiptToProto(ctx, r), nil
	})
}

func orderLinesFromProto(lines []*inventoryv1.PurchaseOrderLine) []usecase.PurchaseOrderLineInput {
	out := make([]usecase.PurchaseOrderLineInput, 0, len(lines))

	for _, l := range lines {
		in := usecase.PurchaseOrderLineInput{ID: l.GetId(), PartID: l.GetPartId(), EnteredQuantity: l.GetEnteredQuantity(), UnitCost: l.GetUnitCost()}
		if l.UnitId != nil {
			in.UnitID = *l.UnitId
		}

		out = append(out, in)
	}

	return out
}

func orderToProto(ctx context.Context, o *domain.PurchaseOrder) *inventoryv1.PurchaseOrder {
	seeCost := canSeeCost(ctx)

	out := &inventoryv1.PurchaseOrder{
		Id: o.ID, GarageId: o.GarageID, Number: o.Number, Status: orderStatusToProto(o.Status), SupplierId: o.SupplierID,
		SupplierName: o.SupplierName, SupplierEmail: o.SupplierEmail, OrderDate: o.OrderDate, ExpectedDate: o.ExpectedDate,
		Notes: o.Notes, Lines: make([]*inventoryv1.PurchaseOrderLine, 0, len(o.Lines)), SentAt: converter.TimePtrToProto(o.SentAt),
		SentTo: o.SentTo, CancelledAt: converter.TimePtrToProto(o.CancelledAt),
		CreatedAt: converter.TimeToProto(o.CreatedAt), UpdatedAt: converter.TimeToProto(o.UpdatedAt),
	}

	if seeCost {
		out.TotalAmount = &o.TotalAmount
	}

	for i := range o.Lines {
		l := &o.Lines[i]
		line := &inventoryv1.PurchaseOrderLine{
			Id: l.ID, PartId: l.PartID, PartSku: l.PartSKU, PartName: l.PartName, BaseUnitAbbreviation: l.BaseUnitAbbreviation,
			UnitId: l.UnitID, UnitAbbreviation: l.UnitAbbreviation, EnteredQuantity: l.EnteredQuantity, Quantity: l.Quantity,
			ReceivedQuantity: l.ReceivedQuantity,
		}

		if seeCost {
			line.UnitCost, line.LineAmount = &l.UnitCost, new(l.Amount())
		}

		out.Lines = append(out.Lines, line)
	}

	return out
}

func orderStatusToProto(s domain.PurchaseOrderStatus) inventoryv1.PurchaseOrderStatus {
	switch s {
	case domain.PurchaseOrderDraft:
		return inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_DRAFT
	case domain.PurchaseOrderSent:
		return inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_SENT
	case domain.PurchaseOrderPartiallyReceived:
		return inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_PARTIALLY_RECEIVED
	case domain.PurchaseOrderReceived:
		return inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_RECEIVED
	case domain.PurchaseOrderCancelled:
		return inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_CANCELLED
	default:
		return inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_UNSPECIFIED
	}
}

func orderStatusFromProto(s inventoryv1.PurchaseOrderStatus) domain.PurchaseOrderStatus {
	switch s {
	case inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_DRAFT:
		return domain.PurchaseOrderDraft
	case inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_SENT:
		return domain.PurchaseOrderSent
	case inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_PARTIALLY_RECEIVED:
		return domain.PurchaseOrderPartiallyReceived
	case inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_RECEIVED:
		return domain.PurchaseOrderReceived
	case inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_CANCELLED:
		return domain.PurchaseOrderCancelled
	case inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}
