package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
)

// AllocateSettlementRequest is the input for SettlementUseCase.Allocate.
type AllocateSettlementRequest struct {
	GarageID         string
	SettlementID     string
	ActorID          string
	CustomerAmount   int64
	InsurerAmount    int64
	InsurerPartnerID *string
}

// Allocate splits the settlement between what the customer pays directly
// and what insurance covers (FR-BIL-02); customerAmount + insurerAmount
// must equal the total, and an insurer is required whenever
// insurerAmount > 0. Re-derives status, since a payment already recorded
// against one payer could now exceed a newly lowered allocation to them
// -- callers see FAILED_PRECONDITION rather than a silently inconsistent
// settlement.
func (uc *SettlementUseCase) Allocate(ctx context.Context, req AllocateSettlementRequest) (*domain.Settlement, error) {
	s, err := uc.Settlements.FindByID(ctx, req.GarageID, req.SettlementID)
	if err != nil {
		return nil, toAppError("allocate settlement", err)
	}

	if err := domain.ValidateAllocation(s.TotalAmount, req.CustomerAmount, req.InsurerAmount, req.InsurerPartnerID); err != nil {
		return nil, toAppError("allocate settlement", err)
	}

	var insurerName string

	if req.InsurerAmount > 0 {
		insurerName, err = uc.Partners.FindInsurer(ctx, req.GarageID, *req.InsurerPartnerID)
		if err != nil {
			return nil, toAppError("allocate settlement", err)
		}
	}

	if req.CustomerAmount < s.CustomerPaidAmount || req.InsurerAmount < s.InsurerPaidAmount {
		return nil, toAppError("allocate settlement", domain.ErrInvalidAllocation)
	}

	s.CustomerAmount, s.InsurerAmount = req.CustomerAmount, req.InsurerAmount
	s.InsurerPartnerID, s.InsurerPartnerName = req.InsurerPartnerID, insurerName
	s.UpdatedBy = actorRef(req.ActorID)
	s.DeriveStatus()

	if err := uc.Settlements.SaveAllocation(ctx, s); err != nil {
		return nil, toAppError("allocate settlement", err)
	}

	return s, nil
}
