package handler

import (
	"context"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

// PayableServer implements receivablev1.PayableServiceServer.
type PayableServer struct {
	receivablev1.UnimplementedPayableServiceServer

	uc *usecase.PayableUseCase
}

// NewPayableServer wires the handler to its use case.
func NewPayableServer(uc *usecase.PayableUseCase) *PayableServer {
	return &PayableServer{uc: uc}
}

// PayablePolicies declares the permission every PayableService RPC requires
// in its garage.
func PayablePolicies() middleware.Policies {
	read, write := access.Requires(access.BillingRead), access.Requires(access.BillingWrite)

	return middleware.Policies{
		receivablev1.PayableService_ListSupplierPayables_FullMethodName:     middleware.GarageMethod(read),
		receivablev1.PayableService_GetSupplierPayablesAging_FullMethodName: middleware.GarageMethod(read),
		receivablev1.PayableService_ListPaymentOutVouchers_FullMethodName:   middleware.GarageMethod(read),
		receivablev1.PayableService_GetPaymentOutVoucher_FullMethodName:     middleware.GarageMethod(read),
		receivablev1.PayableService_CreatePaymentOutVoucher_FullMethodName:  middleware.GarageMethod(write),
		receivablev1.PayableService_VoidPaymentOutVoucher_FullMethodName:    middleware.GarageMethod(write),
	}
}

// SubscribePayables registers the consumers that make and cancel payables
// from the goods receipts of Inventory.
func SubscribePayables(bus outbox.Subscriber, processor *outbox.Processor, uc *usecase.PayableUseCase) {
	bus.Subscribe("goods_receipt.completed", processor.Wrap(ConsumerName+"-payables", uc.HandleReceiptCompleted))
	bus.Subscribe("goods_receipt.cancelled", processor.Wrap(ConsumerName+"-payables-cancel", uc.HandleReceiptCancelled))
}

// ListSupplierPayables lists the payables, oldest first.
func (s *PayableServer) ListSupplierPayables(ctx context.Context, req *receivablev1.ListSupplierPayablesRequest) (*receivablev1.ListSupplierPayablesResponse, error) {
	return run(ctx, func(string) (*receivablev1.ListSupplierPayablesResponse, error) {
		res, err := s.uc.ListPayables(ctx, usecase.ListPayablesRequest{
			GarageID: req.GetGarageId(), SupplierID: req.GetSupplierId(), IncludePaid: req.GetIncludePaid(),
			PageRequest: usecase.PageRequest{Page: int(req.GetPage()), PageSize: int(req.GetPageSize())},
		})
		if err != nil {
			return nil, err
		}

		resp := &receivablev1.ListSupplierPayablesResponse{
			Data:       make([]*receivablev1.SupplierPayable, 0, len(res.Items)),
			Pagination: &commonv1.Pagination{Total: toInt32(res.Total), Page: toInt32(res.Page), PageSize: toInt32(res.PageSize)},
			Summary:    &receivablev1.SupplierPayablesSummary{OpenCount: toInt32(res.Summary.OpenCount), OutstandingAmount: res.Summary.OutstandingAmount},
		}
		for _, item := range res.Items {
			resp.Data = append(resp.Data, payableToProto(item))
		}

		return resp, nil
	})
}

// GetSupplierPayablesAging returns the open payables by supplier and age.
func (s *PayableServer) GetSupplierPayablesAging(ctx context.Context, req *receivablev1.GetSupplierPayablesAgingRequest) (*receivablev1.GetSupplierPayablesAgingResponse, error) {
	return run(ctx, func(string) (*receivablev1.GetSupplierPayablesAgingResponse, error) {
		res, err := s.uc.Aging(ctx, req.GetGarageId())
		if err != nil {
			return nil, err
		}

		resp := &receivablev1.GetSupplierPayablesAgingResponse{
			Suppliers: make([]*receivablev1.SupplierAging, 0, len(res.Suppliers)), Totals: agingToProto(res.Totals),
		}
		for _, a := range res.Suppliers {
			resp.Suppliers = append(resp.Suppliers, agingToProto(a))
		}

		return resp, nil
	})
}

// ListPaymentOutVouchers lists the vouchers, newest first.
func (s *PayableServer) ListPaymentOutVouchers(ctx context.Context, req *receivablev1.ListPaymentOutVouchersRequest) (*receivablev1.ListPaymentOutVouchersResponse, error) {
	return run(ctx, func(string) (*receivablev1.ListPaymentOutVouchersResponse, error) {
		res, err := s.uc.ListVouchers(ctx, usecase.ListVouchersRequest{
			GarageID: req.GetGarageId(), SupplierID: req.GetSupplierId(), IncludeVoided: req.GetIncludeVoided(),
			PageRequest: usecase.PageRequest{Page: int(req.GetPage()), PageSize: int(req.GetPageSize())},
		})
		if err != nil {
			return nil, err
		}

		resp := &receivablev1.ListPaymentOutVouchersResponse{
			Data:       make([]*receivablev1.PaymentOutVoucher, 0, len(res.Items)),
			Pagination: &commonv1.Pagination{Total: toInt32(res.Total), Page: toInt32(res.Page), PageSize: toInt32(res.PageSize)},
		}
		for _, v := range res.Items {
			resp.Data = append(resp.Data, voucherToProto(v))
		}

		return resp, nil
	})
}

// GetPaymentOutVoucher returns one voucher with its allocations.
func (s *PayableServer) GetPaymentOutVoucher(ctx context.Context, req *receivablev1.GetPaymentOutVoucherRequest) (*receivablev1.PaymentOutVoucher, error) {
	return run(ctx, func(string) (*receivablev1.PaymentOutVoucher, error) {
		v, err := s.uc.GetVoucher(ctx, req.GetGarageId(), req.GetVoucherId())
		if err != nil {
			return nil, err
		}

		return voucherToProto(v), nil
	})
}

// CreatePaymentOutVoucher issues a voucher and applies its allocations.
func (s *PayableServer) CreatePaymentOutVoucher(ctx context.Context, req *receivablev1.CreatePaymentOutVoucherRequest) (*receivablev1.PaymentOutVoucher, error) {
	return run(ctx, func(actorID string) (*receivablev1.PaymentOutVoucher, error) {
		in := req.GetVoucher()

		create := usecase.CreateVoucherRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, SupplierID: in.GetSupplierId(), PayeeName: in.GetPayeeName(),
			PayeeAddress: in.GetPayeeAddress(), Amount: in.GetAmount(), Method: methodFromProto(in.GetMethod()),
			Reason: in.GetReason(), Note: in.GetNote(),
		}
		for _, a := range in.GetAllocations() {
			create.Allocations = append(create.Allocations, usecase.AllocationInput{PayableID: a.GetPayableId(), Amount: a.GetAmount()})
		}

		v, err := s.uc.CreateVoucher(ctx, create)
		if err != nil {
			return nil, err
		}

		return voucherToProto(v), nil
	})
}

// VoidPaymentOutVoucher voids a voucher and takes its allocations back.
func (s *PayableServer) VoidPaymentOutVoucher(ctx context.Context, req *receivablev1.VoidPaymentOutVoucherRequest) (*receivablev1.PaymentOutVoucher, error) {
	return run(ctx, func(actorID string) (*receivablev1.PaymentOutVoucher, error) {
		v, err := s.uc.VoidVoucher(ctx, usecase.VoidVoucherRequest{
			GarageID: req.GetGarageId(), VoucherID: req.GetVoucherId(), ActorID: actorID, Reason: req.GetReason(),
		})
		if err != nil {
			return nil, err
		}

		return voucherToProto(v), nil
	})
}
