// Package usecase contains the application use cases of the Repair Order
// module. Release 1.2 only needs CreateFromQuote (FR-QUO-04) and
// ListForVehicle (FR-CUS-03, read by the Customer module through a narrow
// interface, see cmd/server); the Repair Order Service proper (FR-REP-*)
// is built in Release 1.3 on top of the same repository and domain types.
package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

// Repository defines the persistence contract for repair orders. Every
// method only sees live (not soft-deleted) rows of the given garage.
type Repository interface {
	// Create inserts ro and its lines in one statement group; a quote
	// already converted returns domain.ErrRepairOrderNotFound (the unique
	// constraint on quote_id).
	Create(ctx context.Context, ro *domain.RepairOrder) error
	// ListForVehicle returns a vehicle's repair orders, newest first, with
	// no lines (FR-CUS-03 only needs the summary).
	ListForVehicle(ctx context.Context, garageID, vehicleID string) ([]*domain.RepairOrder, error)
}

// CreateFromQuoteRequest is the input for RepairOrderUseCase.CreateFromQuote.
type CreateFromQuoteRequest struct {
	GarageID string
	QuoteID  string
	ActorID  string

	VehicleID string
	// Number is rendered from the garage's repair_order_number_template
	// by the caller (Quote module), which alone knows the garage's
	// numbering configuration.
	Number string
	// Lines are the quote's approved lines (FR-QUO-04); their own line
	// ids are replaced with new ones.
	Lines []domain.Line
}

// RepairOrderUseCase manages the repair orders created from approved
// quotes.
type RepairOrderUseCase struct {
	repo Repository
}

// NewRepairOrderUseCase wires the use case to its repository.
func NewRepairOrderUseCase(repo Repository) *RepairOrderUseCase {
	return &RepairOrderUseCase{repo: repo}
}

// CreateFromQuote creates a draft repair order (status Open) from a
// quote's approved lines (FR-QUO-04). The caller (Quote module) has
// already checked the quote is approved and not yet converted, and runs
// this inside the same transaction as marking the quote converted, so
// both changes commit or roll back together.
func (uc *RepairOrderUseCase) CreateFromQuote(ctx context.Context, req CreateFromQuoteRequest) (*domain.RepairOrder, error) {
	ro := &domain.RepairOrder{
		ID:        uuid.NewString(),
		GarageID:  req.GarageID,
		QuoteID:   &req.QuoteID,
		VehicleID: req.VehicleID,
		Number:    req.Number,
		Status:    domain.StatusOpen,
		CreatedBy: actorRef(req.ActorID),
		UpdatedBy: actorRef(req.ActorID),
	}

	for _, line := range req.Lines {
		line.ID = uuid.NewString()
		ro.TotalAmount += line.LineTotal
		ro.Lines = append(ro.Lines, line)
	}

	if err := uc.repo.Create(ctx, ro); err != nil {
		return nil, err
	}

	return ro, nil
}

// ListForVehicle returns a vehicle's repair orders, newest first
// (FR-CUS-03).
func (uc *RepairOrderUseCase) ListForVehicle(ctx context.Context, garageID, vehicleID string) ([]*domain.RepairOrder, error) {
	return uc.repo.ListForVehicle(ctx, garageID, vehicleID)
}

func actorRef(accountID string) *string {
	if accountID == "" {
		return nil
	}

	return &accountID
}
