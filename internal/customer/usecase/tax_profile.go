package usecase

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
	"github.com/viettechno/vietgara-backend/pkg/email"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// TaxProfileRepository defines the persistence contract for customer tax
// profiles. Every method only sees live rows of the given garage.
type TaxProfileRepository interface {
	// Create inserts p; a second personal profile returns
	// domain.ErrPersonalProfileTaken.
	Create(ctx context.Context, p *domain.TaxProfile) error
	// FindByID returns domain.ErrTaxProfileNotFound when absent or when it
	// belongs to another customer.
	FindByID(ctx context.Context, garageID, customerID, profileID string) (*domain.TaxProfile, error)
	// Update persists the editable fields of p.
	Update(ctx context.Context, p *domain.TaxProfile) error
	// List returns the customer's profiles, default first.
	List(ctx context.Context, garageID, customerID string, includeInactive bool) ([]*domain.TaxProfile, error)
	// ClearDefault removes the default mark of every profile of the customer
	// but exceptID.
	ClearDefault(ctx context.Context, garageID, customerID, exceptID string) error
	// SoftDelete returns domain.ErrTaxProfileNotFound when absent.
	SoftDelete(ctx context.Context, garageID, customerID, profileID, deletedBy string) error
	// SetActive deactivates (which also clears the default mark) or
	// reactivates a profile.
	SetActive(ctx context.Context, garageID, customerID, profileID string, active bool, updatedBy string) error
}

// TaxProfileUsage tells whether a document was billed to a profile, which
// decides between deleting and deactivating it (FR-CAT-07); implemented in
// cmd/server over the modules that own the documents.
type TaxProfileUsage interface {
	TaxProfileInUse(ctx context.Context, garageID, profileID string) (bool, error)
}

// TaxProfileInput is the editable part of a tax profile; blank optional
// fields are stored as absent.
type TaxProfileInput struct {
	LegalName    *string
	TaxID        *string
	Address      *string
	InvoiceEmail *string
	IsDefault    *bool
}

// CreateTaxProfileRequest is the input for TaxProfileUseCase.Create.
type CreateTaxProfileRequest struct {
	GarageID   string
	CustomerID string
	ActorID    string
	Kind       domain.TaxProfileKind
	TaxProfileInput
}

// UpdateTaxProfileRequest is the input for TaxProfileUseCase.Update; nil
// fields keep their value and blank strings clear optional ones.
type UpdateTaxProfileRequest struct {
	GarageID     string
	CustomerID   string
	TaxProfileID string
	ActorID      string
	TaxProfileInput
}

// DeleteTaxProfileRequest is the input for TaxProfileUseCase.Delete.
type DeleteTaxProfileRequest struct {
	GarageID     string
	CustomerID   string
	TaxProfileID string
	ActorID      string
	// DryRun answers the outcome without changing anything.
	DryRun bool
}

// TaxProfileUseCase (FR-CUS-07) manages a customer's tax profiles.
type TaxProfileUseCase struct {
	profiles  TaxProfileRepository
	customers CustomerRepository
	usage     TaxProfileUsage
}

// NewTaxProfileUseCase wires the use case to its repositories and to the
// check of whether documents were billed to a profile.
func NewTaxProfileUseCase(profiles TaxProfileRepository, customers CustomerRepository, usage TaxProfileUsage) *TaxProfileUseCase {
	return &TaxProfileUseCase{profiles: profiles, customers: customers, usage: usage}
}

// normalizeTaxID strips the separators people type ("0301 234 567-001").
func normalizeTaxID(v string) (string, error) {
	id := strings.NewReplacer(" ", "", "-", "", ".", "").Replace(strings.TrimSpace(v))
	if !domain.ValidTaxID(id) {
		return "", domain.ErrInvalidTaxID
	}

	return id, nil
}

func (in TaxProfileInput) apply(p *domain.TaxProfile) error {
	if in.LegalName != nil {
		name, err := required(*in.LegalName, domain.ErrLegalNameRequired, text("legal name", domain.MaxNameLen))
		if err != nil {
			return err
		}

		p.LegalName = name
	}

	if err := patch(&p.TaxID, in.TaxID, normalizeTaxID); err != nil {
		return err
	}

	if err := patch(&p.Address, in.Address, text("address", domain.MaxAddressLen)); err != nil {
		return err
	}

	if err := patch(&p.InvoiceEmail, in.InvoiceEmail, email.Normalize); err != nil {
		return err
	}

	if in.IsDefault != nil {
		p.IsDefault = *in.IsDefault
	}

	return nil
}

// Create validates and stores a new profile for the customer.
func (uc *TaxProfileUseCase) Create(ctx context.Context, req CreateTaxProfileRequest) (*domain.TaxProfile, error) {
	if !req.Kind.IsValid() {
		return nil, toAppError("create tax profile", domain.ErrInvalidTaxProfile)
	}

	if _, err := uc.customers.FindByID(ctx, req.GarageID, req.CustomerID); err != nil {
		return nil, toAppError("create tax profile", err)
	}

	p := &domain.TaxProfile{
		ID: uuid.NewString(), GarageID: req.GarageID, CustomerID: req.CustomerID, Kind: req.Kind, IsActive: true,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	in := req.TaxProfileInput
	if in.LegalName == nil {
		in.LegalName = new("")
	}

	if err := in.apply(p); err != nil {
		return nil, toAppError("create tax profile", err)
	}

	if p.IsDefault {
		if err := uc.profiles.ClearDefault(ctx, req.GarageID, req.CustomerID, p.ID); err != nil {
			return nil, toAppError("create tax profile", err)
		}
	}

	if err := uc.profiles.Create(ctx, p); err != nil {
		return nil, toAppError("create tax profile", err)
	}

	return p, nil
}

// Get returns one profile of the customer.
func (uc *TaxProfileUseCase) Get(ctx context.Context, garageID, customerID, profileID string) (*domain.TaxProfile, error) {
	p, err := uc.profiles.FindByID(ctx, garageID, customerID, profileID)
	if err != nil {
		return nil, toAppError("get tax profile", err)
	}

	return p, nil
}

// List returns the customer's profiles, the default first.
func (uc *TaxProfileUseCase) List(ctx context.Context, garageID, customerID string, includeInactive bool) ([]*domain.TaxProfile, error) {
	if _, err := uc.customers.FindByID(ctx, garageID, customerID); err != nil {
		return nil, toAppError("list tax profiles", err)
	}

	items, err := uc.profiles.List(ctx, garageID, customerID, includeInactive)
	if err != nil {
		return nil, toAppError("list tax profiles", err)
	}

	return items, nil
}

// Update applies the partial updates in req; the kind never changes.
func (uc *TaxProfileUseCase) Update(ctx context.Context, req UpdateTaxProfileRequest) (*domain.TaxProfile, error) {
	p, err := uc.profiles.FindByID(ctx, req.GarageID, req.CustomerID, req.TaxProfileID)
	if err != nil {
		return nil, toAppError("update tax profile", err)
	}

	if err := req.apply(p); err != nil {
		return nil, toAppError("update tax profile", err)
	}

	if p.IsDefault && !p.IsActive {
		p.IsDefault = false
	}

	p.UpdatedBy = actorRef(req.ActorID)

	if p.IsDefault {
		if err := uc.profiles.ClearDefault(ctx, req.GarageID, req.CustomerID, p.ID); err != nil {
			return nil, toAppError("update tax profile", err)
		}
	}

	if err := uc.profiles.Update(ctx, p); err != nil {
		return nil, toAppError("update tax profile", err)
	}

	return p, nil
}

// Resolve returns who a document of the customer is billed to, as the
// snapshot the document keeps (FR-CUS-07). A nil profileID takes the
// customer's default profile, or retail (the zero party) when there is
// none; an empty one is retail explicitly; otherwise it must be an active
// profile of the customer.
func (uc *TaxProfileUseCase) Resolve(ctx context.Context, garageID, customerID string, profileID *string) (billingparty.Party, error) {
	if profileID != nil && *profileID == "" {
		return billingparty.Party{}, nil
	}

	if profileID == nil {
		profiles, err := uc.profiles.List(ctx, garageID, customerID, false)
		if err != nil {
			return billingparty.Party{}, toAppError("resolve the billing party", err)
		}

		for _, p := range profiles {
			if p.IsDefault {
				return partyOf(p), nil
			}
		}

		return billingparty.Party{}, nil
	}

	p, err := uc.profiles.FindByID(ctx, garageID, customerID, *profileID)
	if errors.Is(err, domain.ErrTaxProfileNotFound) || (err == nil && !p.IsActive) {
		return billingparty.Party{}, toAppError("resolve the billing party", domain.ErrBillingProfileUnusable)
	}

	if err != nil {
		return billingparty.Party{}, toAppError("resolve the billing party", err)
	}

	return partyOf(p), nil
}

// partyOf takes the snapshot of a profile.
func partyOf(p *domain.TaxProfile) billingparty.Party {
	party := billingparty.Party{TaxProfileID: p.ID, Kind: string(p.Kind), LegalName: p.LegalName}

	if p.TaxID != nil {
		party.TaxID = *p.TaxID
	}

	if p.Address != nil {
		party.Address = *p.Address
	}

	if p.InvoiceEmail != nil {
		party.InvoiceEmail = *p.InvoiceEmail
	}

	return party
}

// Delete follows the delete-or-deactivate rule (FR-CAT-07): a profile that a
// document was billed to is deactivated.
func (uc *TaxProfileUseCase) Delete(ctx context.Context, req DeleteTaxProfileRequest) (lifecycle.Outcome, error) {
	if _, err := uc.profiles.FindByID(ctx, req.GarageID, req.CustomerID, req.TaxProfileID); err != nil {
		return "", toAppError("delete tax profile", err)
	}

	outcome, err := lifecycle.Apply(ctx, req.DryRun, lifecycle.Rule{
		InUse: func(ctx context.Context) (bool, error) {
			return uc.usage.TaxProfileInUse(ctx, req.GarageID, req.TaxProfileID)
		},
		Delete: func(ctx context.Context) error {
			return uc.profiles.SoftDelete(ctx, req.GarageID, req.CustomerID, req.TaxProfileID, req.ActorID)
		},
		Deactivate: func(ctx context.Context) error {
			return uc.profiles.SetActive(ctx, req.GarageID, req.CustomerID, req.TaxProfileID, false, req.ActorID)
		},
	})
	if err != nil {
		return "", toAppError("delete tax profile", err)
	}

	return outcome, nil
}

// Activate reactivates a deactivated profile.
func (uc *TaxProfileUseCase) Activate(ctx context.Context, garageID, customerID, profileID, actorID string) (*domain.TaxProfile, error) {
	if err := uc.profiles.SetActive(ctx, garageID, customerID, profileID, true, actorID); err != nil {
		return nil, toAppError("activate tax profile", err)
	}

	return uc.Get(ctx, garageID, customerID, profileID)
}
