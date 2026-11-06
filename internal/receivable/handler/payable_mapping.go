package handler

import (
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func payableStatusToProto(s domain.PayableStatus) receivablev1.PayableStatus {
	switch s {
	case domain.PayableOpen:
		return receivablev1.PayableStatus_PAYABLE_STATUS_OPEN
	case domain.PayablePaid:
		return receivablev1.PayableStatus_PAYABLE_STATUS_PAID
	case domain.PayableCancelled:
		return receivablev1.PayableStatus_PAYABLE_STATUS_CANCELLED
	default:
		return receivablev1.PayableStatus_PAYABLE_STATUS_UNSPECIFIED
	}
}

func payableToProto(item usecase.PayableItem) *receivablev1.SupplierPayable {
	p := item.Payable

	return &receivablev1.SupplierPayable{
		Id: p.ID, GarageId: p.GarageID, GoodsReceiptId: p.GoodsReceiptID, GoodsReceiptNumber: p.GoodsReceiptNumber,
		SupplierId: p.SupplierID, SupplierName: p.SupplierName, ReceiptDate: p.ReceiptDate.Format("2006-01-02"),
		TotalAmount: p.TotalAmount, PaidAmount: p.PaidAmount, RemainingAmount: p.Remaining(), AgeDays: toInt32(item.AgeDays),
		Status: payableStatusToProto(p.Status), CreatedAt: converter.TimeToProto(p.CreatedAt),
	}
}

func agingToProto(a domain.Aging) *receivablev1.SupplierAging {
	return &receivablev1.SupplierAging{
		SupplierId: a.SupplierID, SupplierName: a.SupplierName, Total: a.Total, Current: a.Current,
		Days_31_60: a.Days31to60, Days_61_90: a.Days61to90, Over_90: a.Over90,
	}
}

func methodFromProto(m billingv1.PaymentMethod) string {
	switch m {
	case billingv1.PaymentMethod_PAYMENT_METHOD_CASH:
		return "cash"
	case billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER:
		return "bank_transfer"
	case billingv1.PaymentMethod_PAYMENT_METHOD_ONLINE:
		return "online"
	case billingv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func voucherToProto(v *domain.PaymentOutVoucher) *receivablev1.PaymentOutVoucher {
	out := &receivablev1.PaymentOutVoucher{
		Id: v.ID, GarageId: v.GarageID, Number: v.Number, SupplierId: v.SupplierID, SupplierName: v.SupplierName,
		PayeeName: v.PayeeName, PayeeAddress: v.PayeeAddress, Amount: v.Amount, Method: methodToProto(v.Method),
		Reason: v.Reason, Note: v.Note, UnallocatedAmount: v.Unallocated(), IssuerName: v.IssuerName,
		Voided: v.Voided(), VoidReason: v.VoidReason, VoidedAt: timePtr(v.VoidedAt), CreatedAt: converter.TimeToProto(v.CreatedAt),
		Allocations: make([]*receivablev1.PaymentOutAllocation, 0, len(v.Allocations)),
	}

	for _, a := range v.Allocations {
		out.Allocations = append(out.Allocations, &receivablev1.PaymentOutAllocation{
			PayableId: a.PayableID, GoodsReceiptNumber: a.GoodsReceiptNumber, Amount: a.Amount,
		})
	}

	return out
}
