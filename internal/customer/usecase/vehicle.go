package usecase

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
)

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
}

// VehicleUseCase (FR-CUS-02) manages the vehicles of a garage's customers.
type VehicleUseCase struct {
	vehicles  VehicleRepository
	customers CustomerRepository
	opts      options
}

// NewVehicleUseCase wires the vehicle use case to its repositories.
func NewVehicleUseCase(vehicles VehicleRepository, customers CustomerRepository, opts ...Option) *VehicleUseCase {
	return &VehicleUseCase{vehicles: vehicles, customers: customers, opts: newOptions(opts)}
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

	items, total, err := uc.vehicles.List(ctx, req.GarageID, filter)
	if err != nil {
		return nil, toAppError("list vehicles", err)
	}

	return &Page[*domain.Vehicle]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// Delete soft-deletes a vehicle; quotes and repair orders keep referencing
// the row.
func (uc *VehicleUseCase) Delete(ctx context.Context, garageID, vehicleID, actorID string) error {
	if err := uc.vehicles.SoftDelete(ctx, garageID, vehicleID, actorID); err != nil {
		return toAppError("delete vehicle", err)
	}

	return nil
}
