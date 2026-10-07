package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/numbering"
)

const docTypeSettlement = "settlement"

// EventSettlementCreated is published by the Billing & Settlement module
// (Event Design Section 4).
const EventSettlementCreated = "settlement.created"

// Deps are the dependencies of SettlementUseCase.
type Deps struct {
	Settlements  Repository
	RepairOrders RepairOrderReader
	Partners     PartnerReader
	Garages      GarageReader
	Limits       TransactionLimiter
	Sequences    Sequencer
	Events       EventPublisher
	Audit        AuditLogger
	// Now defaults to time.Now.
	Now clock
}

// SettlementUseCase implements every action of the Billing & Settlement
// module.
type SettlementUseCase struct {
	Deps
}

// NewSettlementUseCase wires the use case.
func NewSettlementUseCase(deps Deps) *SettlementUseCase {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &SettlementUseCase{Deps: deps}
}

// CreateSettlementRequest is the input for SettlementUseCase.Create.
type CreateSettlementRequest struct {
	GarageID      string
	ActorID       string
	RepairOrderID string
}

// Create creates a settlement from a closed repair order (FR-BIL-01),
// aggregating its total; the whole amount starts allocated to the
// customer until AllocateSettlement says otherwise. Counts against the
// plan's daily transaction limit (FR-LIC-02), the same as quote and
// repair order creation.
func (uc *SettlementUseCase) Create(ctx context.Context, req CreateSettlementRequest) (*domain.Settlement, error) {
	switch _, err := uc.Settlements.FindByRepairOrderID(ctx, req.GarageID, req.RepairOrderID); {
	case err == nil:
		return nil, toAppError("create settlement", domain.ErrAlreadySettled)
	case !errors.Is(err, domain.ErrSettlementNotFound):
		return nil, toAppError("create settlement", err)
	}

	ro, err := uc.RepairOrders.Get(ctx, req.GarageID, req.RepairOrderID)
	if err != nil {
		return nil, toAppError("create settlement", err)
	}

	if !ro.Closed {
		return nil, toAppError("create settlement", domain.ErrRepairOrderNotClosed)
	}

	garage, err := uc.garageInfo(ctx, req.GarageID)
	if err != nil {
		return nil, err
	}

	if err := uc.Limits.ConsumeTransaction(ctx, garage.OwnerAccountID); err != nil {
		return nil, err
	}

	number, err := uc.nextNumber(ctx, req.GarageID, garage)
	if err != nil {
		return nil, toAppError("create settlement", err)
	}

	s := &domain.Settlement{
		ID: uuid.NewString(), GarageID: req.GarageID, RepairOrderID: req.RepairOrderID,
		VehicleLicensePlate: ro.VehicleLicensePlate, CustomerName: ro.CustomerName, Number: number,
		TotalAmount: ro.TotalAmount, CustomerAmount: ro.TotalAmount, Status: domain.StatusPending,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}
	s.DeriveStatus()

	if err := uc.Settlements.Create(ctx, s); err != nil {
		return nil, toAppError("create settlement", err)
	}

	if err := uc.Events.Publish(ctx, EventSettlementCreated, req.GarageID, "", map[string]any{
		"settlement_id": s.ID, "garage_id": req.GarageID, "repair_order_id": req.RepairOrderID,
	}); err != nil {
		return nil, toAppError("create settlement", err)
	}

	if err := uc.audit(ctx, req.ActorID, req.GarageID, ActionSettlementCreated, entitySettlement, s.ID, nil, settlementAudit(s)); err != nil {
		return nil, err
	}

	return s, nil
}

// Get returns one settlement of the garage, with its payments.
func (uc *SettlementUseCase) Get(ctx context.Context, garageID, settlementID string) (*domain.Settlement, error) {
	s, err := uc.Settlements.FindByID(ctx, garageID, settlementID)
	if err != nil {
		return nil, toAppError("get settlement", err)
	}

	return s, nil
}

// ListSettlementsRequest is the input for SettlementUseCase.List.
type ListSettlementsRequest struct {
	PageRequest

	GarageID        string
	Status          domain.SettlementStatus
	OutstandingOnly bool
}

// List returns one page of the garage's settlements matching req. Also
// FR-REC-01's receivables view, via OutstandingOnly.
func (uc *SettlementUseCase) List(ctx context.Context, req ListSettlementsRequest) (*Page[*domain.Settlement], error) {
	page := req.normalize()
	filter := ListFilter{
		Status: req.Status, OutstandingOnly: req.OutstandingOnly && req.Status == "",
		Limit: page.PageSize, Offset: page.offset(),
	}

	items, total, err := uc.Settlements.List(ctx, req.GarageID, filter)
	if err != nil {
		return nil, toAppError("list settlements", err)
	}

	return &Page[*domain.Settlement]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// garageInfo reads the garage's numbering and owner configuration.
func (uc *SettlementUseCase) garageInfo(ctx context.Context, garageID string) (GarageInfo, error) {
	garage, err := uc.Garages.Get(ctx, garageID)
	if err != nil {
		return GarageInfo{}, apperror.Internal("read garage", err)
	}

	return garage, nil
}

// nextNumber reserves and renders the settlement's document number from
// the garage's settlement_number_template (FR-TEN-05), in the garage's
// timezone.
func (uc *SettlementUseCase) nextNumber(ctx context.Context, garageID string, garage GarageInfo) (string, error) {
	now := uc.Now()
	if loc, err := time.LoadLocation(garage.Timezone); err == nil {
		now = now.In(loc)
	}

	period := numbering.Period(garage.SettlementNumberTemplate, now)

	seq, err := uc.Sequences.Next(ctx, garageID, docTypeSettlement, period)
	if err != nil {
		return "", err
	}

	return numbering.Render(garage.SettlementNumberTemplate, now, seq), nil
}
