package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/supplier/domain"
	"github.com/viettechno/vietgara-backend/pkg/email"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
	"github.com/viettechno/vietgara-backend/pkg/phone"
)

// ContactInput is the contact document of a supplier as entered; blank
// fields are stored as absent.
type ContactInput struct {
	ContactPerson *string
	Phone         *string
	Email         *string
	Address       *string
}

func (in ContactInput) normalize() (domain.Contact, error) {
	var (
		out domain.Contact
		err error
	)

	if out.ContactPerson, err = optional(in.ContactPerson, text("contact person", domain.MaxNameLen)); err != nil {
		return out, err
	}

	if out.Phone, err = optional(in.Phone, phone.Normalize); err != nil {
		return out, err
	}

	if out.Email, err = optional(in.Email, email.Normalize); err != nil {
		return out, err
	}

	out.Address, err = optional(in.Address, text("address", domain.MaxAddressLen))

	return out, err
}

// BankInput is the bank account of a supplier as entered; blank fields are
// stored as absent.
type BankInput struct {
	BankName      *string
	AccountNumber *string
	AccountHolder *string
}

func (in BankInput) normalize() (domain.BankDetails, error) {
	var (
		out domain.BankDetails
		err error
	)

	if out.BankName, err = optional(in.BankName, text("bank name", domain.MaxBankLen)); err != nil {
		return out, err
	}

	if out.AccountNumber, err = optional(in.AccountNumber, text("account number", domain.MaxBankLen)); err != nil {
		return out, err
	}

	out.AccountHolder, err = optional(in.AccountHolder, text("account holder", domain.MaxNameLen))

	return out, err
}

// CreateRequest is the input for UseCase.Create.
type CreateRequest struct {
	GarageID string
	ActorID  string
	Name     string
	TaxCode  *string
	Contact  ContactInput
	Bank     BankInput
	Note     *string
}

// UpdateRequest is the input for UseCase.Update; nil fields keep their
// current value, blank strings clear optional ones, and a non-nil Contact
// or Bank replaces the whole document.
type UpdateRequest struct {
	GarageID   string
	SupplierID string
	ActorID    string
	Name       *string
	TaxCode    *string
	Contact    *ContactInput
	Bank       *BankInput
	Note       *string
}

// ListRequest is the input for UseCase.List.
type ListRequest struct {
	PageRequest

	GarageID string
	// Query matches name, tax code, contact person, phone or email.
	Query string
	// IncludeInactive also lists deactivated suppliers.
	IncludeInactive bool
}

// DeleteRequest is the input for UseCase.Delete.
type DeleteRequest struct {
	GarageID   string
	SupplierID string
	ActorID    string
	// DryRun answers the outcome without changing anything.
	DryRun bool
}

// UseCase (FR-SUP-01) manages the garage's directory of suppliers.
type UseCase struct {
	suppliers Repository
	usage     UsageChecker
}

// NewUseCase wires the supplier use case to its repository and to the
// check of whether documents still refer to a supplier.
func NewUseCase(suppliers Repository, usage UsageChecker) *UseCase {
	return &UseCase{suppliers: suppliers, usage: usage}
}

// fields is the editable part of a supplier shared by create and update;
// nil fields are left untouched.
type fields struct {
	Name    *string
	TaxCode *string
	Contact *ContactInput
	Bank    *BankInput
	Note    *string
}

func (f fields) apply(s *domain.Supplier) error {
	if f.Name != nil {
		name, err := required(*f.Name, domain.ErrSupplierNameRequired, text("name", domain.MaxNameLen))
		if err != nil {
			return err
		}

		s.Name = name
	}

	if f.Contact != nil {
		contact, err := f.Contact.normalize()
		if err != nil {
			return err
		}

		s.Contact = contact
	}

	if f.Bank != nil {
		bank, err := f.Bank.normalize()
		if err != nil {
			return err
		}

		s.Bank = bank
	}

	if err := patch(&s.TaxCode, f.TaxCode, code("tax code", domain.MaxTaxCodeLen)); err != nil {
		return err
	}

	return patch(&s.Note, f.Note, text("note", domain.MaxNoteLen))
}

// Create validates and stores a new supplier.
func (uc *UseCase) Create(ctx context.Context, req CreateRequest) (*domain.Supplier, error) {
	s := &domain.Supplier{
		ID:        uuid.NewString(),
		GarageID:  req.GarageID,
		CreatedBy: actorRef(req.ActorID),
		UpdatedBy: actorRef(req.ActorID),
	}

	f := fields{Name: &req.Name, TaxCode: req.TaxCode, Contact: &req.Contact, Bank: &req.Bank, Note: req.Note}
	if err := f.apply(s); err != nil {
		return nil, toAppError("create supplier", err)
	}

	if err := uc.suppliers.Create(ctx, s); err != nil {
		return nil, toAppError("create supplier", err)
	}

	return s, nil
}

// Get returns one supplier of the garage.
func (uc *UseCase) Get(ctx context.Context, garageID, supplierID string) (*domain.Supplier, error) {
	s, err := uc.suppliers.FindByID(ctx, garageID, supplierID)
	if err != nil {
		return nil, toAppError("get supplier", err)
	}

	return s, nil
}

// Update applies the partial updates in req to a supplier.
func (uc *UseCase) Update(ctx context.Context, req UpdateRequest) (*domain.Supplier, error) {
	s, err := uc.suppliers.FindByID(ctx, req.GarageID, req.SupplierID)
	if err != nil {
		return nil, toAppError("update supplier", err)
	}

	f := fields{Name: req.Name, TaxCode: req.TaxCode, Contact: req.Contact, Bank: req.Bank, Note: req.Note}
	if err := f.apply(s); err != nil {
		return nil, toAppError("update supplier", err)
	}

	s.UpdatedBy = actorRef(req.ActorID)

	if err := uc.suppliers.Update(ctx, s); err != nil {
		return nil, toAppError("update supplier", err)
	}

	return s, nil
}

// List returns one page of the garage's suppliers matching req.
func (uc *UseCase) List(ctx context.Context, req ListRequest) (*Page[*domain.Supplier], error) {
	page := req.normalize()

	filter := page.filter(req.Query)
	filter.IncludeInactive = req.IncludeInactive

	items, total, err := uc.suppliers.List(ctx, req.GarageID, filter)
	if err != nil {
		return nil, toAppError("list suppliers", err)
	}

	return &Page[*domain.Supplier]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// Delete follows the delete-or-deactivate rule (FR-CAT-07): a supplier that
// documents refer to is deactivated; otherwise it is soft-deleted, which
// frees its name.
func (uc *UseCase) Delete(ctx context.Context, req DeleteRequest) (lifecycle.Outcome, error) {
	if _, err := uc.suppliers.FindByID(ctx, req.GarageID, req.SupplierID); err != nil {
		return "", toAppError("delete supplier", err)
	}

	outcome, err := lifecycle.Apply(ctx, req.DryRun, lifecycle.Rule{
		InUse: func(ctx context.Context) (bool, error) {
			return uc.usage.SupplierInUse(ctx, req.GarageID, req.SupplierID)
		},
		Delete: func(ctx context.Context) error {
			return uc.suppliers.SoftDelete(ctx, req.GarageID, req.SupplierID, req.ActorID)
		},
		Deactivate: func(ctx context.Context) error {
			return uc.suppliers.SetActive(ctx, req.GarageID, req.SupplierID, false, req.ActorID)
		},
	})
	if err != nil {
		return "", toAppError("delete supplier", err)
	}

	return outcome, nil
}

// Activate reactivates a deactivated supplier.
func (uc *UseCase) Activate(ctx context.Context, garageID, supplierID, actorID string) (*domain.Supplier, error) {
	if err := uc.suppliers.SetActive(ctx, garageID, supplierID, true, actorID); err != nil {
		return nil, toAppError("activate supplier", err)
	}

	return uc.Get(ctx, garageID, supplierID)
}
