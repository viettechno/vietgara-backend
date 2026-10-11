package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// HistoryEntryType names which module produced a vehicle history entry
// (FR-CUS-03).
type HistoryEntryType string

// Valid HistoryEntryType values.
const (
	HistoryEntryQuote       HistoryEntryType = "quote"
	HistoryEntryRepairOrder HistoryEntryType = "repair_order"
	HistoryEntrySettlement  HistoryEntryType = "settlement"
)

// HistoryEntry is one quote, repair order or settlement of a vehicle
// (FR-CUS-03).
type HistoryEntry struct {
	Type        HistoryEntryType
	EntityID    string
	Number      string
	Status      string
	TotalAmount int64
	OccurredAt  time.Time
}

// HistoryReader collects a vehicle's repair history from the modules that
// own it (Quote & Pricing today; Repair Order and Billing & Settlement in
// Releases 1.3-1.4); implemented by an adapter in cmd/server so Customer
// never imports those modules directly.
type HistoryReader interface {
	History(ctx context.Context, garageID, vehicleID string) ([]HistoryEntry, error)
}

// CreateVehicleRequest is the input for VehicleUseCase.Create.
type CreateVehicleRequest struct {
	GarageID     string
	ActorID      string
	CustomerID   string
	LicensePlate string
	Make         *string
	Model        *string
	// Year is the model year; nil or 0 leaves it unknown.
	Year         *int
	VIN          *string
	EngineNumber *string
}

// UpdateVehicleRequest is the input for VehicleUseCase.Update; nil fields
// keep their current value, blank strings (or a 0 year) clear optional ones,
// and CustomerID transfers the vehicle to another customer.
type UpdateVehicleRequest struct {
	GarageID     string
	VehicleID    string
	ActorID      string
	CustomerID   *string
	LicensePlate *string
	Make         *string
	Model        *string
	Year         *int
	VIN          *string
	EngineNumber *string
}

// ListVehiclesRequest is the input for VehicleUseCase.List.
type ListVehiclesRequest struct {
	PageRequest

	GarageID string
	// CustomerID restricts the list to one customer's vehicles when set.
	CustomerID string
	// Query matches plate (ignoring separators), make, model or VIN.
	Query string
	// IncludeInactive also lists deactivated vehicles.
	IncludeInactive bool
}

// DeleteVehicleRequest is the input for VehicleUseCase.Delete.
type DeleteVehicleRequest struct {
	GarageID  string
	VehicleID string
	ActorID   string
	// DryRun answers the outcome without changing anything.
	DryRun bool
}

// VehicleUseCase (FR-CUS-02) manages the vehicles of a garage's customers.
type VehicleUseCase struct {
	vehicles  VehicleRepository
	customers CustomerRepository
	history   HistoryReader
	usage     UsageChecker
	opts      options
}

// NewVehicleUseCase wires the vehicle use case to its repositories. history
// may be nil, in which case GetHistory always returns an empty list (unit
// tests that do not exercise FR-CUS-03 need not provide one). usage tells
// whether documents still refer to a vehicle (FR-CAT-07).
func NewVehicleUseCase(
	vehicles VehicleRepository, customers CustomerRepository, history HistoryReader, usage UsageChecker, opts ...Option,
) *VehicleUseCase {
	return &VehicleUseCase{vehicles: vehicles, customers: customers, history: history, usage: usage, opts: newOptions(opts)}
}

// vehicleFields is the editable part of a vehicle shared by create and
// update; nil fields are left untouched.
type vehicleFields struct {
	LicensePlate *string
	Make         *string
	Model        *string
	Year         *int
	VIN          *string
	EngineNumber *string
}

func (uc *VehicleUseCase) apply(v *domain.Vehicle, f vehicleFields) error {
	if f.LicensePlate != nil {
		plate, err := domain.NormalizeLicensePlate(*f.LicensePlate)
		if err != nil {
			return err
		}

		v.LicensePlate = plate
	}

	if err := uc.applyYear(v, f.Year); err != nil {
		return err
	}

	for _, field := range []struct {
		dst **string
		src *string
		n   normalizer
	}{
		{&v.Make, f.Make, text("make", domain.MaxMakeLen)},
		{&v.Model, f.Model, text("model", domain.MaxMakeLen)},
		{&v.VIN, f.VIN, code("VIN", domain.MaxCodeLen)},
		{&v.EngineNumber, f.EngineNumber, code("engine number", domain.MaxCodeLen)},
	} {
		if err := patch(field.dst, field.src, field.n); err != nil {
			return err
		}
	}

	return nil
}

func (uc *VehicleUseCase) applyYear(v *domain.Vehicle, year *int) error {
	switch {
	case year == nil:
		return nil
	case *year == 0:
		v.Year = nil

		return nil
	default:
		if err := domain.ValidateYear(*year, uc.opts.now()); err != nil {
			return err
		}

		y := *year
		v.Year = &y

		return nil
	}
}

// Create registers a vehicle to an existing customer of the garage.
func (uc *VehicleUseCase) Create(ctx context.Context, req CreateVehicleRequest) (*domain.Vehicle, error) {
	v := &domain.Vehicle{
		ID:        uuid.NewString(),
		GarageID:  req.GarageID,
		CreatedBy: actorRef(req.ActorID),
		UpdatedBy: actorRef(req.ActorID),
	}

	if strings.TrimSpace(req.LicensePlate) == "" {
		return nil, toAppError("create vehicle", domain.ErrLicensePlateRequired)
	}

	fields := vehicleFields{
		LicensePlate: &req.LicensePlate, Make: req.Make, Model: req.Model,
		Year: req.Year, VIN: req.VIN, EngineNumber: req.EngineNumber,
	}
	if err := uc.apply(v, fields); err != nil {
		return nil, toAppError("create vehicle", err)
	}

	if err := uc.assignOwner(ctx, v, req.CustomerID); err != nil {
		return nil, err
	}

	if err := uc.vehicles.Create(ctx, v); err != nil {
		return nil, toAppError("create vehicle", err)
	}

	return v, nil
}

// assignOwner links v to a live customer of the same garage.
func (uc *VehicleUseCase) assignOwner(ctx context.Context, v *domain.Vehicle, customerID string) error {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return toAppError("find vehicle owner", domain.ErrCustomerRequired)
	}

	owner, err := uc.customers.FindByID(ctx, v.GarageID, customerID)
	if err != nil {
		return toAppError("find vehicle owner", err)
	}

	v.CustomerID, v.CustomerName = owner.ID, owner.FullName

	return nil
}

// Get returns one vehicle of the garage.
func (uc *VehicleUseCase) Get(ctx context.Context, garageID, vehicleID string) (*domain.Vehicle, error) {
	v, err := uc.vehicles.FindByID(ctx, garageID, vehicleID)
	if err != nil {
		return nil, toAppError("get vehicle", err)
	}

	return v, nil
}

// Update applies the partial updates in req to a vehicle.
func (uc *VehicleUseCase) Update(ctx context.Context, req UpdateVehicleRequest) (*domain.Vehicle, error) {
	v, err := uc.vehicles.FindByID(ctx, req.GarageID, req.VehicleID)
	if err != nil {
		return nil, toAppError("update vehicle", err)
	}

	fields := vehicleFields{
		LicensePlate: req.LicensePlate, Make: req.Make, Model: req.Model,
		Year: req.Year, VIN: req.VIN, EngineNumber: req.EngineNumber,
	}
	if err := uc.apply(v, fields); err != nil {
		return nil, toAppError("update vehicle", err)
	}

	if req.CustomerID != nil && *req.CustomerID != v.CustomerID {
		if err := uc.assignOwner(ctx, v, *req.CustomerID); err != nil {
			return nil, err
		}
	}

	v.UpdatedBy = actorRef(req.ActorID)

	if err := uc.vehicles.Update(ctx, v); err != nil {
		return nil, toAppError("update vehicle", err)
	}

	return v, nil
}

// List returns one page of the garage's vehicles matching req.
func (uc *VehicleUseCase) List(ctx context.Context, req ListVehiclesRequest) (*Page[*domain.Vehicle], error) {
	page := req.normalize()
	filter := VehicleFilter{ListFilter: page.filter(req.Query), CustomerID: strings.TrimSpace(req.CustomerID)}
	filter.IncludeInactive = req.IncludeInactive

	items, total, err := uc.vehicles.List(ctx, req.GarageID, filter)
	if err != nil {
		return nil, toAppError("list vehicles", err)
	}

	return &Page[*domain.Vehicle]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// Delete follows the delete-or-deactivate rule (FR-CAT-07): a vehicle that
// quotes or repair orders refer to is deactivated, so its history stays;
// otherwise it is soft-deleted.
func (uc *VehicleUseCase) Delete(ctx context.Context, req DeleteVehicleRequest) (lifecycle.Outcome, error) {
	if _, err := uc.vehicles.FindByID(ctx, req.GarageID, req.VehicleID); err != nil {
		return "", toAppError("delete vehicle", err)
	}

	outcome, err := lifecycle.Apply(ctx, req.DryRun, lifecycle.Rule{
		InUse: func(ctx context.Context) (bool, error) {
			return uc.usage.VehicleInUse(ctx, req.GarageID, req.VehicleID)
		},
		Delete: func(ctx context.Context) error {
			return uc.vehicles.SoftDelete(ctx, req.GarageID, req.VehicleID, req.ActorID)
		},
		Deactivate: func(ctx context.Context) error {
			return uc.vehicles.SetActive(ctx, req.GarageID, req.VehicleID, false, req.ActorID)
		},
	})
	if err != nil {
		return "", toAppError("delete vehicle", err)
	}

	return outcome, nil
}

// Activate reactivates a deactivated vehicle.
func (uc *VehicleUseCase) Activate(ctx context.Context, garageID, vehicleID, actorID string) (*domain.Vehicle, error) {
	if err := uc.vehicles.SetActive(ctx, garageID, vehicleID, true, actorID); err != nil {
		return nil, toAppError("activate vehicle", err)
	}

	return uc.Get(ctx, garageID, vehicleID)
}

// GetHistory returns a vehicle's quotes, repair orders and settlements,
// newest first (FR-CUS-03). The vehicle must exist in the garage; an empty
// slice (not an error) means it simply has no history yet.
func (uc *VehicleUseCase) GetHistory(ctx context.Context, garageID, vehicleID string) ([]HistoryEntry, error) {
	if _, err := uc.vehicles.FindByID(ctx, garageID, vehicleID); err != nil {
		return nil, toAppError("get vehicle history", err)
	}

	if uc.history == nil {
		return nil, nil
	}

	entries, err := uc.history.History(ctx, garageID, vehicleID)
	if err != nil {
		return nil, toAppError("get vehicle history", err)
	}

	return entries, nil
}
