package usecase

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/pkg/email"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
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
	// Locale is the language of the customer's e-mails; empty means vi.
	Locale string
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
	Locale              *string
	// EmailOptOut records the customer's choice about non-essential e-mails.
	EmailOptOut *bool
}

// ListCustomersRequest is the input for CustomerUseCase.List.
type ListCustomersRequest struct {
	PageRequest

	GarageID string
	// Query matches name, phone, email or one of the customer's plates.
	Query string
	// IncludeInactive also lists deactivated customers.
	IncludeInactive bool
}

// DeleteCustomerRequest is the input for CustomerUseCase.Delete.
type DeleteCustomerRequest struct {
	GarageID   string
	CustomerID string
	ActorID    string
	// DryRun answers the outcome without changing anything.
	DryRun bool
}

// CustomerUseCase (FR-CUS-01) manages the customer profiles of a garage.
type CustomerUseCase struct {
	customers CustomerRepository
	vehicles  VehicleRepository
	usage     UsageChecker
	opts      options
}

// NewCustomerUseCase wires the customer use case to its repositories and to
// the check of whether documents still refer to a customer.
func NewCustomerUseCase(customers CustomerRepository, vehicles VehicleRepository, usage UsageChecker, opts ...Option) *CustomerUseCase {
	return &CustomerUseCase{customers: customers, vehicles: vehicles, usage: usage, opts: newOptions(opts)}
}

// customerFields is the editable part of a customer shared by create and
// update; nil fields are left untouched.
type customerFields struct {
	FullName *string
	Phone    *string
	Email    *string
	Address  *string
	Locale   *string
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

	if f.Locale != nil {
		locale := strings.ToLower(strings.TrimSpace(*f.Locale))

		switch locale {
		case "":
			locale = domain.LocaleVI
		case domain.LocaleVI, domain.LocaleEN:
		default:
			return domain.ErrInvalidLocale
		}

		c.Locale = locale
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

	locale := req.Locale
	fields := customerFields{FullName: &req.FullName, Phone: req.Phone, Email: req.Email, Address: req.Address, Locale: &locale}

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

	fields := customerFields{FullName: req.FullName, Phone: req.Phone, Email: req.Email, Address: req.Address, Locale: req.Locale}
	if err := fields.apply(c); err != nil {
		return nil, toAppError("update customer", err)
	}

	if req.PersonalDataConsent != nil {
		c.RecordConsent(*req.PersonalDataConsent, uc.opts.now().UTC(), req.ActorID)
	}

	if req.EmailOptOut != nil {
		c.SetEmailOptOut(*req.EmailOptOut, uc.opts.now().UTC(), req.ActorID)
	}

	c.UpdatedBy = actorRef(req.ActorID)

	if err := uc.customers.Update(ctx, c); err != nil {
		return nil, toAppError("update customer", err)
	}

	return c, nil
}

// OptOutEmail records, in the consent log, that the customer opted out of
// non-essential e-mails by themselves (the link in an e-mail). It is
// idempotent.
func (uc *CustomerUseCase) OptOutEmail(ctx context.Context, garageID, customerID string) error {
	c, err := uc.customers.FindByID(ctx, garageID, customerID)
	if err != nil {
		return toAppError("opt out of e-mail", err)
	}

	if !c.SetEmailOptOut(true, uc.opts.now().UTC(), "") {
		return nil
	}

	if err := uc.customers.Update(ctx, c); err != nil {
		return toAppError("opt out of e-mail", err)
	}

	return nil
}

// List returns one page of the garage's customers matching req.Query.
func (uc *CustomerUseCase) List(ctx context.Context, req ListCustomersRequest) (*Page[*domain.Customer], error) {
	page := req.normalize()

	filter := page.filter(req.Query)
	filter.IncludeInactive = req.IncludeInactive

	items, total, err := uc.customers.List(ctx, req.GarageID, filter)
	if err != nil {
		return nil, toAppError("list customers", err)
	}

	return &Page[*domain.Customer]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// Delete follows the delete-or-deactivate rule (FR-CAT-07): a customer that
// quotes or repair orders refer to (directly or through their vehicles) is
// deactivated, so those documents and the vehicle history stay; otherwise
// the customer and their vehicles are soft-deleted.
func (uc *CustomerUseCase) Delete(ctx context.Context, req DeleteCustomerRequest) (lifecycle.Outcome, error) {
	if _, err := uc.customers.FindByID(ctx, req.GarageID, req.CustomerID); err != nil {
		return "", toAppError("delete customer", err)
	}

	outcome, err := lifecycle.Apply(ctx, req.DryRun, lifecycle.Rule{
		InUse: func(ctx context.Context) (bool, error) {
			vehicleIDs, err := uc.vehicles.IDsByCustomer(ctx, req.GarageID, req.CustomerID)
			if err != nil {
				return false, err
			}

			return uc.usage.CustomerInUse(ctx, req.GarageID, req.CustomerID, vehicleIDs)
		},
		Delete: func(ctx context.Context) error {
			if err := uc.customers.SoftDelete(ctx, req.GarageID, req.CustomerID, req.ActorID); err != nil {
				return err
			}

			return uc.vehicles.SoftDeleteByCustomer(ctx, req.GarageID, req.CustomerID, req.ActorID)
		},
		Deactivate: func(ctx context.Context) error {
			return uc.customers.SetActive(ctx, req.GarageID, req.CustomerID, false, req.ActorID)
		},
	})
	if err != nil {
		return "", toAppError("delete customer", err)
	}

	return outcome, nil
}

// Activate reactivates a deactivated customer.
func (uc *CustomerUseCase) Activate(ctx context.Context, garageID, customerID, actorID string) (*domain.Customer, error) {
	if err := uc.customers.SetActive(ctx, garageID, customerID, true, actorID); err != nil {
		return nil, toAppError("activate customer", err)
	}

	return uc.Get(ctx, garageID, customerID)
}
