package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
)

// RecordPaymentRequest is the input for SettlementUseCase.RecordPayment.
type RecordPaymentRequest struct {
	GarageID     string
	SettlementID string
	ActorID      string
	PayerType    domain.PayerType
	Amount       int64
	Method       domain.PaymentMethod
	Note         string
}

// RecordPayment records a payment actually collected (FR-BIL-03); a
// settlement may receive several, from either payer, until each payer's
// allocated amount is fully collected.
func (uc *SettlementUseCase) RecordPayment(ctx context.Context, req RecordPaymentRequest) (*domain.Settlement, error) {
	if !req.PayerType.IsValid() {
		return nil, toAppError("record payment", domain.ErrInvalidPayerType)
	}

	if !req.Method.IsValid() {
		return nil, toAppError("record payment", domain.ErrInvalidPaymentMethod)
	}

	s, err := uc.Settlements.FindByID(ctx, req.GarageID, req.SettlementID)
	if err != nil {
		return nil, toAppError("record payment", err)
	}

	if req.Amount <= 0 || req.Amount > s.RemainingDue(req.PayerType) {
		return nil, toAppError("record payment", domain.ErrInvalidPaymentAmount)
	}

	payment := &domain.Payment{
		ID: uuid.NewString(), PayerType: req.PayerType, Amount: req.Amount, Method: req.Method, Note: req.Note,
		CreatedBy: actorRef(req.ActorID),
	}

	updated, err := uc.Settlements.RecordPayment(ctx, req.GarageID, req.SettlementID, payment)
	if err != nil {
		return nil, toAppError("record payment", err)
	}

	updated.DeriveStatus()

	if err := uc.Settlements.SaveStatus(ctx, req.GarageID, req.SettlementID, updated.Status); err != nil {
		return nil, toAppError("record payment", err)
	}

	return uc.Get(ctx, req.GarageID, req.SettlementID)
}
