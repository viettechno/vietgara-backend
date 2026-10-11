package handler

import (
	"math"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func settlementToProto(s *domain.Settlement) *billingv1.Settlement {
	out := &billingv1.Settlement{
		Id: s.ID, GarageId: s.GarageID, RepairOrderId: s.RepairOrderID, VehicleLicensePlate: s.VehicleLicensePlate,
		CustomerName: s.CustomerName, Number: s.Number, TotalAmount: s.TotalAmount,
		PaidAmount: s.PaidAmount,
		Status:     statusToProto(s.Status), Payments: make([]*billingv1.SettlementPayment, 0, len(s.Payments)),
		CreatedAt: converter.TimeToProto(s.CreatedAt), UpdatedAt: converter.TimeToProto(s.UpdatedAt),
	}

	for _, p := range s.Payments {
		out.Payments = append(out.Payments, paymentToProto(p))
	}

	return out
}

func paymentToProto(p domain.Payment) *billingv1.SettlementPayment {
	return &billingv1.SettlementPayment{
		Id: p.ID, Amount: p.Amount, Method: methodToProto(p.Method),
		Note: p.Note, CreatedAt: converter.TimeToProto(p.CreatedAt),
		VoucherNumber: p.VoucherNumber, PayerName: p.PayerName, PayerAddress: p.PayerAddress, Reason: p.Reason,
		CollectorName: p.CollectorName,
	}
}

func statusToProto(s domain.SettlementStatus) billingv1.SettlementStatus {
	switch s {
	case domain.StatusPending:
		return billingv1.SettlementStatus_SETTLEMENT_STATUS_PENDING
	case domain.StatusPartiallyPaid:
		return billingv1.SettlementStatus_SETTLEMENT_STATUS_PARTIALLY_PAID
	case domain.StatusPaid:
		return billingv1.SettlementStatus_SETTLEMENT_STATUS_PAID
	default:
		return billingv1.SettlementStatus_SETTLEMENT_STATUS_UNSPECIFIED
	}
}

// statusFromProto maps UNSPECIFIED (and unknown values) to "", which the
// use case treats as "every status" when listing.
func statusFromProto(s billingv1.SettlementStatus) domain.SettlementStatus {
	switch s {
	case billingv1.SettlementStatus_SETTLEMENT_STATUS_PENDING:
		return domain.StatusPending
	case billingv1.SettlementStatus_SETTLEMENT_STATUS_PARTIALLY_PAID:
		return domain.StatusPartiallyPaid
	case billingv1.SettlementStatus_SETTLEMENT_STATUS_PAID:
		return domain.StatusPaid
	case billingv1.SettlementStatus_SETTLEMENT_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func methodToProto(m domain.PaymentMethod) billingv1.PaymentMethod {
	switch m {
	case domain.MethodCash:
		return billingv1.PaymentMethod_PAYMENT_METHOD_CASH
	case domain.MethodBankTransfer:
		return billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER
	case domain.MethodOnline:
		return billingv1.PaymentMethod_PAYMENT_METHOD_ONLINE
	default:
		return billingv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED
	}
}

func methodFromProto(m billingv1.PaymentMethod) domain.PaymentMethod {
	switch m {
	case billingv1.PaymentMethod_PAYMENT_METHOD_CASH:
		return domain.MethodCash
	case billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER:
		return domain.MethodBankTransfer
	case billingv1.PaymentMethod_PAYMENT_METHOD_ONLINE:
		return domain.MethodOnline
	case billingv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func paginationToProto(p *usecase.Page[*domain.Settlement]) *commonv1.Pagination {
	return &commonv1.Pagination{Total: toInt32(p.Total), Page: toInt32(p.Page), PageSize: toInt32(p.PageSize)}
}

func pageRequest(page, pageSize int32) usecase.PageRequest {
	return usecase.PageRequest{Page: int(page), PageSize: int(pageSize)}
}

func toInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}

	if n < math.MinInt32 {
		return math.MinInt32
	}

	return int32(n)
}
