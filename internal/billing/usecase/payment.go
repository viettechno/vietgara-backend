package usecase

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
)

// RecordPaymentRequest is the input for SettlementUseCase.RecordPayment.
type RecordPaymentRequest struct {
	GarageID     string
	SettlementID string
	ActorID      string
	Amount       int64
	Method       domain.PaymentMethod
	Note         string
	// PayerName, PayerAddress and Reason are the voucher's; each defaults
	// to the billing party's (else the customer's) name, its address and a
	// standard reason.
	PayerName    string
	PayerAddress string
	Reason       string
}

// RecordPayment records a payment actually collected (FR-BIL-03); a
// settlement may receive several until the total is fully collected.
func (uc *SettlementUseCase) RecordPayment(ctx context.Context, req RecordPaymentRequest) (*domain.Settlement, error) {
	if !req.Method.IsValid() {
		return nil, toAppError("record payment", domain.ErrInvalidPaymentMethod)
	}

	s, err := uc.Settlements.FindByID(ctx, req.GarageID, req.SettlementID)
	if err != nil {
		return nil, toAppError("record payment", err)
	}

	if req.Amount <= 0 || req.Amount > s.RemainingDue() {
		return nil, toAppError("record payment", domain.ErrInvalidPaymentAmount)
	}

	before := settlementAudit(s)

	payment, err := uc.issueVoucher(ctx, s, req)
	if err != nil {
		return nil, err
	}

	updated, err := uc.Settlements.RecordPayment(ctx, req.GarageID, req.SettlementID, payment)
	if err != nil {
		return nil, toAppError("record payment", err)
	}

	updated.DeriveStatus()

	if err := uc.Settlements.SaveStatus(ctx, req.GarageID, req.SettlementID, updated.Status); err != nil {
		return nil, toAppError("record payment", err)
	}

	// The payment is the entity; the settlement's totals and status before
	// and after it are what an auditor reconciles.
	after := settlementAudit(updated)
	after["payment"] = paymentAudit(payment)

	if err := uc.audit(ctx, req.ActorID, req.GarageID, ActionPaymentRecorded, entityPayment, payment.ID, before, after); err != nil {
		return nil, err
	}

	return uc.Get(ctx, req.GarageID, req.SettlementID)
}

// VoidPaymentRequest is the input for SettlementUseCase.VoidPayment.
type VoidPaymentRequest struct {
	GarageID     string
	SettlementID string
	PaymentID    string
	ActorID      string
	Reason       string
}

// VoidPayment voids a payment voucher that was recorded by mistake
// (FR-BIL-06): the voucher stays on file with its number and the reason,
// the settlement's paid amount and status are recalculated, and the audit
// log records who voided it. A voucher is never deleted.
func (uc *SettlementUseCase) VoidPayment(ctx context.Context, req VoidPaymentRequest) (*domain.Settlement, error) {
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > domain.MaxVoidReasonLength {
		return nil, toAppError("void payment", domain.ErrInvalidVoidReason)
	}

	s, err := uc.Settlements.FindByID(ctx, req.GarageID, req.SettlementID)
	if err != nil {
		return nil, toAppError("void payment", err)
	}

	payment := findPayment(s, req.PaymentID)
	if payment == nil {
		return nil, toAppError("void payment", domain.ErrPaymentNotFound)
	}

	before := settlementAudit(s)
	before["payment"] = paymentAudit(payment)

	updated, err := uc.Settlements.VoidPayment(ctx, req.GarageID, req.SettlementID, req.PaymentID, reason)
	if err != nil {
		return nil, toAppError("void payment", err)
	}

	updated.DeriveStatus()

	if err := uc.Settlements.SaveStatus(ctx, req.GarageID, req.SettlementID, updated.Status); err != nil {
		return nil, toAppError("void payment", err)
	}

	after := settlementAudit(updated)
	after["payment"] = map[string]any{"voucher_number": payment.VoucherNumber, "amount": payment.Amount, "voided": true, "void_reason": reason}

	if err := uc.audit(ctx, req.ActorID, req.GarageID, ActionPaymentVoided, entityPayment, req.PaymentID, before, after); err != nil {
		return nil, err
	}

	return uc.Get(ctx, req.GarageID, req.SettlementID)
}

func findPayment(s *domain.Settlement, paymentID string) *domain.Payment {
	for i := range s.Payments {
		if s.Payments[i].ID == paymentID {
			return &s.Payments[i]
		}
	}

	return nil
}

// issueVoucher builds the payment with its payment voucher (FR-BIL-05): the
// next number from the garage's template, who paid and why, who collected,
// and the garage's legal identity as it is now.
func (uc *SettlementUseCase) issueVoucher(ctx context.Context, s *domain.Settlement, req RecordPaymentRequest) (*domain.Payment, error) {
	payerName := firstNonBlank(req.PayerName, s.BillingParty.LegalName, s.CustomerName)
	payerAddress := firstNonBlank(req.PayerAddress, s.BillingParty.Address)
	reason := firstNonBlank(req.Reason, "Thanh toán quyết toán "+s.Number)

	if utf8.RuneCountInString(payerName) > domain.MaxPayerNameLength ||
		utf8.RuneCountInString(payerAddress) > domain.MaxPayerAddressLength ||
		utf8.RuneCountInString(reason) > domain.MaxReasonLength {
		return nil, toAppError("record payment", domain.ErrInvalidVoucher)
	}

	garage, err := uc.garageInfo(ctx, req.GarageID)
	if err != nil {
		return nil, err
	}

	number, err := uc.nextNumber(ctx, req.GarageID, docTypePaymentVoucher, garage.VoucherNumberTemplate, garage.Timezone)
	if err != nil {
		return nil, toAppError("record payment", err)
	}

	collector, err := uc.Collectors.FullName(ctx, req.ActorID)
	if err != nil {
		return nil, toAppError("record payment", err)
	}

	return &domain.Payment{
		ID: uuid.NewString(), Amount: req.Amount, Method: req.Method, Note: req.Note, VoucherNumber: number,
		PayerName: payerName, PayerAddress: payerAddress, Reason: reason, CollectorName: collector, Issuer: garage.Issuer,
		CreatedBy: actorRef(req.ActorID),
	}, nil
}

// firstNonBlank returns the first value that is not blank, trimmed.
func firstNonBlank(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}

	return ""
}
