package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/numbering"
)

const docTypeRepairOrder = "repair_order"

// Events published by the Repair Order module (Event Design Section 4).
const (
	EventRepairOrderClosed = "repair_order.closed"
	keyGarageID            = "garage_id"
)

// Deps are the dependencies of RepairOrderUseCase.
type Deps struct {
	RepairOrders Repository
	Vehicles     VehicleReader
	Garages      GarageReader
	Limits       TransactionLimiter
	Sequences    Sequencer
	Events       EventPublisher
	// Now defaults to time.Now.
	Now clock
}

// RepairOrderUseCase implements every action of the Repair Order module.
type RepairOrderUseCase struct {
	Deps
}

// NewRepairOrderUseCase wires the use case.
func NewRepairOrderUseCase(deps Deps) *RepairOrderUseCase {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &RepairOrderUseCase{Deps: deps}
}

// LineInput is one line of a create request.
type LineInput struct {
	ItemType    domain.ItemType
	ItemRefID   *string
	Description string
	Quantity    string
	UnitPrice   int64
}

// CreateRepairOrderRequest is the input for RepairOrderUseCase.Create.
type CreateRepairOrderRequest struct {
	GarageID  string
	ActorID   string
	VehicleID string
	Lines     []LineInput
}

// Create creates an ad-hoc repair order (FR-REP-01), numbered from the
// garage's repair_order_number_template and counted against the plan's
// daily transaction limit (FR-LIC-02).
func (uc *RepairOrderUseCase) Create(ctx context.Context, req CreateRepairOrderRequest) (*domain.RepairOrder, error) {
	garage, err := uc.garageInfo(ctx, req.GarageID)
	if err != nil {
		return nil, err
	}

	lines, err := buildLines(req.Lines)
	if err != nil {
		return nil, toAppError("create repair order", err)
	}

	customerName, plate, err := uc.Vehicles.Find(ctx, req.GarageID, req.VehicleID)
	if err != nil {
		return nil, toAppError("create repair order", err)
	}

	if err := uc.Limits.ConsumeTransaction(ctx, garage.OwnerUserID); err != nil {
		return nil, err
	}

	number, err := uc.nextNumber(ctx, req.GarageID, garage)
	if err != nil {
		return nil, toAppError("create repair order", err)
	}

	ro := &domain.RepairOrder{
		ID: uuid.NewString(), GarageID: req.GarageID, VehicleID: req.VehicleID, VehicleLicensePlate: plate,
		CustomerName: customerName, Number: number, Status: domain.StatusOpen, Lines: lines,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}
	ro.Recalculate()

	if err := uc.RepairOrders.Create(ctx, ro); err != nil {
		return nil, toAppError("create repair order", err)
	}

	return ro, nil
}

// CreateFromQuoteRequest is the input for RepairOrderUseCase.CreateFromQuote.
type CreateFromQuoteRequest struct {
	GarageID string
	QuoteID  string
	ActorID  string

	VehicleID    string
	CustomerName string
	VehiclePlate string
	// Number is rendered from the garage's repair_order_number_template
	// by the caller (Quote module), which alone knows the garage's
	// numbering configuration.
	Number string
	// Lines are the quote's approved lines (FR-QUO-04); their own line
	// ids are replaced with new ones.
	Lines []domain.Line
}

// CreateFromQuote creates a draft repair order (status Open) from a
// quote's approved lines (FR-QUO-04). The caller (Quote module) has
// already checked the quote is approved and not yet converted, and runs
// this inside the same transaction as marking the quote converted, so
// both changes commit or roll back together.
func (uc *RepairOrderUseCase) CreateFromQuote(ctx context.Context, req CreateFromQuoteRequest) (*domain.RepairOrder, error) {
	ro := &domain.RepairOrder{
		ID: uuid.NewString(), GarageID: req.GarageID, QuoteID: &req.QuoteID, VehicleID: req.VehicleID,
		VehicleLicensePlate: req.VehiclePlate, CustomerName: req.CustomerName, Number: req.Number, Status: domain.StatusOpen,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	for _, line := range req.Lines {
		line.ID = uuid.NewString()
		line.Status = domain.LineNotStarted
		ro.Lines = append(ro.Lines, line)
	}

	ro.Recalculate()

	if err := uc.RepairOrders.Create(ctx, ro); err != nil {
		return nil, err
	}

	return ro, nil
}

// Get returns one repair order of the garage, with its lines.
func (uc *RepairOrderUseCase) Get(ctx context.Context, garageID, repairOrderID string) (*domain.RepairOrder, error) {
	ro, err := uc.RepairOrders.FindByID(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError("get repair order", err)
	}

	return ro, nil
}

// ListRepairOrdersRequest is the input for RepairOrderUseCase.List.
type ListRepairOrdersRequest struct {
	PageRequest

	GarageID     string
	VehicleID    string
	Status       domain.RepairOrderStatus
	TechnicianID string
}

// List returns one page of the garage's repair orders matching req (the
// Kanban board's data).
func (uc *RepairOrderUseCase) List(ctx context.Context, req ListRepairOrdersRequest) (*Page[*domain.RepairOrder], error) {
	page := req.normalize()
	filter := ListFilter{
		VehicleID: req.VehicleID, Status: req.Status, TechnicianID: req.TechnicianID,
		Limit: page.PageSize, Offset: page.offset(),
	}

	items, total, err := uc.RepairOrders.List(ctx, req.GarageID, filter)
	if err != nil {
		return nil, toAppError("list repair orders", err)
	}

	return &Page[*domain.RepairOrder]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// ListForVehicle returns a vehicle's repair orders, newest first
// (FR-CUS-03).
func (uc *RepairOrderUseCase) ListForVehicle(ctx context.Context, garageID, vehicleID string) ([]*domain.RepairOrder, error) {
	return uc.RepairOrders.ListForVehicle(ctx, garageID, vehicleID)
}

// garageInfo reads the garage's numbering and owner configuration.
func (uc *RepairOrderUseCase) garageInfo(ctx context.Context, garageID string) (GarageInfo, error) {
	garage, err := uc.Garages.Get(ctx, garageID)
	if err != nil {
		return GarageInfo{}, apperror.Internal("read garage", err)
	}

	return garage, nil
}

// nextNumber reserves and renders the repair order's document number from
// the garage's repair_order_number_template (FR-TEN-05), in the garage's
// timezone.
func (uc *RepairOrderUseCase) nextNumber(ctx context.Context, garageID string, garage GarageInfo) (string, error) {
	now := uc.Now()
	if loc, err := time.LoadLocation(garage.Timezone); err == nil {
		now = now.In(loc)
	}

	period := numbering.Period(garage.RepairOrderNumberTemplate, now)

	seq, err := uc.Sequences.Next(ctx, garageID, docTypeRepairOrder, period)
	if err != nil {
		return "", err
	}

	return numbering.Render(garage.RepairOrderNumberTemplate, now, seq), nil
}

// buildLines validates inputs and assigns each line an id, position,
// status and computed total.
func buildLines(inputs []LineInput) ([]domain.Line, error) {
	if len(inputs) == 0 {
		return nil, domain.ErrNoLines
	}

	if len(inputs) > domain.MaxLines {
		return nil, domain.ErrTooManyLines
	}

	lines := make([]domain.Line, 0, len(inputs))

	for i, in := range inputs {
		description := strings.TrimSpace(in.Description)
		if description == "" {
			return nil, domain.ErrDescriptionRequired
		}

		if err := domain.CheckLength(description, domain.MaxDescriptionLen); err != nil {
			return nil, err
		}

		if !in.ItemType.IsValid() {
			return nil, domain.ErrInvalidItemType
		}

		if in.UnitPrice < 0 {
			return nil, domain.ErrInvalidUnitPrice
		}

		scaled, err := domain.ParseQuantity(in.Quantity)
		if err != nil {
			return nil, err
		}

		lines = append(lines, domain.Line{
			ID: uuid.NewString(), Position: i + 1, ItemType: in.ItemType, ItemRefID: in.ItemRefID,
			Description: description, Quantity: in.Quantity, UnitPrice: in.UnitPrice,
			LineTotal: domain.LineTotal(scaled, in.UnitPrice), Status: domain.LineNotStarted,
		})
	}

	return lines, nil
}
