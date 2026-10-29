package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
	"github.com/viettechno/vietgara-backend/pkg/numbering"
)

const (
	docTypeSettlement     = "settlement"
	docTypePaymentVoucher = "payment_voucher"
)

// EventSettlementCreated is published by the Billing & Settlement module
// (Event Design Section 4).
const EventSettlementCreated = "settlement.created"

// Events published when a payment is recorded (Release 10.1): the voucher
// issued for every payment, and the settlement paid by the payment that
// brings its balance to zero, once.
const (
	EventPaymentVoucherIssued = "payment_voucher.issued"
	EventSettlementPaid       = "settlement.paid"
)

// Deps are the dependencies of SettlementUseCase.
type Deps struct {
	Settlements    Repository
	RepairOrders   RepairOrderReader
	BillingParties BillingPartyResolver
	Garages        GarageReader
	Collectors     CollectorReader
	Limits         TransactionLimiter
	Sequences      Sequencer
	Events         EventPublisher
	Audit          AuditLogger
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
	// BillingTaxProfileID is the customer's tax profile to bill: nil takes
	// the repair order's party, "" is a retail receipt.
	BillingTaxProfileID *string
	// InvoiceRequested asks for a VAT invoice, which needs a complete party.
	InvoiceRequested bool
}

// Create creates a settlement from a closed repair order (FR-BIL-01),
// aggregating its total. Counts against the plan's daily transaction
// limit (FR-LIC-02), the same as quote and repair order creation.
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

	party, err := uc.billingParty(ctx, req, ro)
	if err != nil {
		return nil, err
	}

	garage, err := uc.garageInfo(ctx, req.GarageID)
	if err != nil {
		return nil, err
	}

	if err := uc.Limits.ConsumeTransaction(ctx, garage.OwnerUserID); err != nil {
		return nil, err
	}

	number, err := uc.nextNumber(ctx, req.GarageID, docTypeSettlement, garage.SettlementNumberTemplate, garage.Timezone)
	if err != nil {
		return nil, toAppError("create settlement", err)
	}

	s := &domain.Settlement{
		ID: uuid.NewString(), GarageID: req.GarageID, RepairOrderID: req.RepairOrderID,
		VehicleLicensePlate: ro.VehicleLicensePlate, CustomerName: ro.CustomerName, Number: number,
		TotalAmount: ro.TotalAmount, Status: domain.StatusPending, Issuer: garage.Issuer, BillingParty: party,
		InvoiceRequested: req.InvoiceRequested,
		CreatedBy:        actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
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

// billingParty is who the new settlement is billed to: the profile the
// request chose, else the repair order's party, else (an ad-hoc repair order
// that was never billed to anyone) the customer's default profile.
func (uc *SettlementUseCase) billingParty(
	ctx context.Context, req CreateSettlementRequest, ro RepairOrderInfo,
) (billingparty.Party, error) {
	party, err := uc.chooseParty(ctx, req, ro)
	if err != nil {
		return billingparty.Party{}, err
	}

	return party, checkInvoice("create settlement", req.InvoiceRequested, party)
}

func (uc *SettlementUseCase) chooseParty(
	ctx context.Context, req CreateSettlementRequest, ro RepairOrderInfo,
) (billingparty.Party, error) {
	if req.BillingTaxProfileID == nil && !ro.BillingParty.IsZero() {
		return ro.BillingParty, nil
	}

	party, err := uc.BillingParties.Resolve(ctx, req.GarageID, ro.VehicleID, req.BillingTaxProfileID)
	if err != nil {
		return billingparty.Party{}, toAppError("create settlement", err)
	}

	return party, nil
}

// checkInvoice refuses a VAT invoice for a party without a legal name, tax
// id and address.
func checkInvoice(op string, requested bool, party billingparty.Party) error {
	if requested && !party.InvoiceReady() {
		return toAppError(op, domain.ErrInvoiceDataIncomplete)
	}

	return nil
}

// UpdateSettlementRequest is the input for SettlementUseCase.Update; only
// who is billed and whether an invoice is requested can change.
type UpdateSettlementRequest struct {
	GarageID     string
	SettlementID string
	ActorID      string
	// BillingTaxProfileID, when non-nil, changes the billing party ("" is a
	// retail receipt).
	BillingTaxProfileID *string
	InvoiceRequested    *bool
}

// Update changes who the settlement is billed to and whether a VAT invoice
// is requested (FR-CUS-07); the invoice needs a complete billing party.
func (uc *SettlementUseCase) Update(ctx context.Context, req UpdateSettlementRequest) (*domain.Settlement, error) {
	s, err := uc.Settlements.FindByID(ctx, req.GarageID, req.SettlementID)
	if err != nil {
		return nil, toAppError("update settlement", err)
	}

	before := settlementAudit(s)

	if req.BillingTaxProfileID != nil {
		ro, err := uc.RepairOrders.Get(ctx, req.GarageID, s.RepairOrderID)
		if err != nil {
			return nil, toAppError("update settlement", err)
		}

		party, err := uc.BillingParties.Resolve(ctx, req.GarageID, ro.VehicleID, req.BillingTaxProfileID)
		if err != nil {
			return nil, toAppError("update settlement", err)
		}

		s.BillingParty = party
	}

	if req.InvoiceRequested != nil {
		s.InvoiceRequested = *req.InvoiceRequested
	}

	if err := checkInvoice("update settlement", s.InvoiceRequested, s.BillingParty); err != nil {
		return nil, err
	}

	s.UpdatedBy = actorRef(req.ActorID)

	if err := uc.Settlements.SaveBilling(ctx, s); err != nil {
		return nil, toAppError("update settlement", err)
	}

	if err := uc.audit(ctx, req.ActorID, req.GarageID, ActionBillingUpdated, entitySettlement, s.ID, before, settlementAudit(s)); err != nil {
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

// nextNumber reserves and renders the next document number of docType from
// the garage's numbering template (FR-TEN-05), in the garage's timezone.
func (uc *SettlementUseCase) nextNumber(ctx context.Context, garageID, docType, template, timezone string) (string, error) {
	now := uc.Now()
	if loc, err := time.LoadLocation(timezone); err == nil {
		now = now.In(loc)
	}

	period := numbering.Period(template, now)

	seq, err := uc.Sequences.Next(ctx, garageID, docType, period)
	if err != nil {
		return "", err
	}

	return numbering.Render(template, now, seq), nil
}
