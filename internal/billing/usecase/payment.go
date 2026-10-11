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
	// to the customer's name, no address and a standard reason.
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

// issueVoucher builds the payment with its payment voucher (FR-BIL-05): the
// next number from the garage's template, who paid and why, who collected,
// and the garage's legal identity as it is now.
func (uc *SettlementUseCase) issueVoucher(ctx context.Context, s *domain.Settlement, req RecordPaymentRequest) (*domain.Payment, error) {
	payerName := firstNonBlank(req.PayerName, s.CustomerName)
	payerAddress := strings.TrimSpace(req.PayerAddress)
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
