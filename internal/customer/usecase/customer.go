package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/pkg/email"
	"github.com/viettechno/vietgara-backend/pkg/phone"
)

// CreateCustomerRequest is the input for CustomerUseCase.Create.
type CreateCustomerRequest struct {
	GarageID string
	// ActorID is the user performing the change, kept for auditing.
	ActorID             string
	FullName            string
	Phone               *string
	Email               *string
	Address             *string
	PersonalDataConsent bool
}

// UpdateCustomerRequest is the input for CustomerUseCase.Update; nil
// fields keep their current value and blank strings clear optional ones.
type UpdateCustomerRequest struct {
	GarageID            string
	CustomerID          string
	ActorID             string
	FullName            *string
	Phone               *string
	Email               *string
	Address             *string
	PersonalDataConsent *bool
}

// ListCustomersRequest is the input for CustomerUseCase.List.
type ListCustomersRequest struct {
	PageRequest

	GarageID string
	// Query matches name, phone, email or one of the customer's plates.
	Query string
}

// CustomerUseCase (FR-CUS-01) manages the customer profiles of a garage.
type CustomerUseCase struct {
	customers CustomerRepository
	vehicles  VehicleRepository
	opts      options
}

// NewCustomerUseCase wires the customer use case to its repositories.
func NewCustomerUseCase(customers CustomerRepository, vehicles VehicleRepository, opts ...Option) *CustomerUseCase {
	return &CustomerUseCase{customers: customers, vehicles: vehicles, opts: newOptions(opts)}
}

// customerFields is the editable part of a customer shared by create and
// update; nil fields are left untouched.
type customerFields struct {
	FullName *string
	Phone    *string
	Email    *string
	Address  *string
}

func (f customerFields) apply(c *domain.Customer) error {
	if f.FullName != nil {
		name, err := required(*f.FullName, domain.ErrFullNameRequired, text("full name", domain.MaxNameLen))
		if err != nil {
			return err
		}

		c.FullName = name
	}

	if err := patch(&c.Phone, f.Phone, phone.Normalize); err != nil {
		return err
	}

	if err := patch(&c.Email, f.Email, email.Normalize); err != nil {
		return err
	}

	return patch(&c.Address, f.Address, text("address", domain.MaxAddressLen))
}

// Create validates and stores a new customer, recording their initial
// personal-data consent decision.
func (uc *CustomerUseCase) Create(ctx context.Context, req CreateCustomerRequest) (*domain.Customer, error) {
	c := &domain.Customer{
		ID:        uuid.NewString(),
		GarageID:  req.GarageID,
		CreatedBy: actorRef(req.ActorID),
		UpdatedBy: actorRef(req.ActorID),
	}

	fields := customerFields{FullName: &req.FullName, Phone: req.Phone, Email: req.Email, Address: req.Address}
	if err := fields.apply(c); err != nil {
		return nil, toAppError("create customer", err)
	}

	c.RecordConsent(req.PersonalDataConsent, uc.opts.now().UTC(), req.ActorID)

	if err := uc.customers.Create(ctx, c); err != nil {
		return nil, toAppError("create customer", err)
	}

	return c, nil
}

// Get returns one customer of the garage.
func (uc *CustomerUseCase) Get(ctx context.Context, garageID, customerID string) (*domain.Customer, error) {
	c, err := uc.customers.FindByID(ctx, garageID, customerID)
	if err != nil {
		return nil, toAppError("get customer", err)
	}

	return c, nil
}

// Update applies the partial updates in req to a customer.
func (uc *CustomerUseCase) Update(ctx context.Context, req UpdateCustomerRequest) (*domain.Customer, error) {
	c, err := uc.customers.FindByID(ctx, req.GarageID, req.CustomerID)
	if err != nil {
		return nil, toAppError("update customer", err)
	}

	fields := customerFields{FullName: req.FullName, Phone: req.Phone, Email: req.Email, Address: req.Address}
	if err := fields.apply(c); err != nil {
		return nil, toAppError("update customer", err)
	}

	if req.PersonalDataConsent != nil {
		c.RecordConsent(*req.PersonalDataConsent, uc.opts.now().UTC(), req.ActorID)
	}

	c.UpdatedBy = actorRef(req.ActorID)

	if err := uc.customers.Update(ctx, c); err != nil {
		return nil, toAppError("update customer", err)
	}

	return c, nil
}

// List returns one page of the garage's customers matching req.Query.
func (uc *CustomerUseCase) List(ctx context.Context, req ListCustomersRequest) (*Page[*domain.Customer], error) {
	page := req.normalize()

	items, total, err := uc.customers.List(ctx, req.GarageID, page.filter(req.Query))
	if err != nil {
		return nil, toAppError("list customers", err)
	}

	return &Page[*domain.Customer]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// Delete soft-deletes a customer together with their vehicles; quotes and
// repair orders keep referencing the rows.
func (uc *CustomerUseCase) Delete(ctx context.Context, garageID, customerID, actorID string) error {
	if err := uc.customers.SoftDelete(ctx, garageID, customerID, actorID); err != nil {
		return toAppError("delete customer", err)
	}

	if err := uc.vehicles.SoftDeleteByCustomer(ctx, garageID, customerID, actorID); err != nil {
		return toAppError("delete customer vehicles", err)
	}

	return nil
}
