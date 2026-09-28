package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
)

// PartnerContactInput is the contact document of a partner as entered;
// blank fields are stored as absent.
type PartnerContactInput struct {
	ContactPerson *string
	Phone         *string
	Email         *string
	Address       *string
}

func (in PartnerContactInput) normalize() (domain.PartnerContact, error) {
	var (
		out domain.PartnerContact
		err error
	)

	if out.ContactPerson, err = optional(in.ContactPerson, text("contact person", domain.MaxNameLen)); err != nil {
		return out, err
	}

	if out.Phone, err = optional(in.Phone, domain.NormalizePhone); err != nil {
		return out, err
	}

	if out.Email, err = optional(in.Email, domain.NormalizeEmail); err != nil {
		return out, err
	}

	out.Address, err = optional(in.Address, text("address", domain.MaxAddressLen))

	return out, err
}

// CreatePartnerRequest is the input for PartnerUseCase.Create.
type CreatePartnerRequest struct {
	GarageID string
	ActorID  string
	Type     domain.PartnerType
	Name     string
	TaxCode  *string
	Contact  PartnerContactInput
	Note     *string
}

// UpdatePartnerRequest is the input for PartnerUseCase.Update; nil fields
// keep their current value, blank strings clear optional ones, and a
// non-nil Contact replaces the whole contact document.
type UpdatePartnerRequest struct {
	GarageID  string
	PartnerID string
	ActorID   string
	Type      *domain.PartnerType
	Name      *string
	TaxCode   *string
	Contact   *PartnerContactInput
	Note      *string
}

// ListPartnersRequest is the input for PartnerUseCase.List.
type ListPartnersRequest struct {
	PageRequest

	GarageID string
	// Type restricts the list to one partner type when set.
	Type domain.PartnerType
	// Query matches name, tax code, contact person, phone or email.
	Query string
}

// PartnerUseCase (FR-CUS-04) manages the garage's directory of parts
// suppliers and insurers.
type PartnerUseCase struct {
	partners PartnerRepository
}

// NewPartnerUseCase wires the partner use case to its repository.
func NewPartnerUseCase(partners PartnerRepository) *PartnerUseCase {
	return &PartnerUseCase{partners: partners}
}

// partnerFields is the editable part of a partner shared by create and
// update; nil fields are left untouched.
type partnerFields struct {
	Type    *domain.PartnerType
	Name    *string
	TaxCode *string
	Contact *PartnerContactInput
	Note    *string
}

func (f partnerFields) apply(p *domain.Partner) error {
	if f.Type != nil {
		if !f.Type.IsValid() {
			return domain.ErrInvalidPartnerType
		}

		p.Type = *f.Type
	}

	if f.Name != nil {
		name, err := required(*f.Name, domain.ErrPartnerNameRequired, text("name", domain.MaxNameLen))
		if err != nil {
			return err
		}

		p.Name = name
	}

	if f.Contact != nil {
		contact, err := f.Contact.normalize()
		if err != nil {
			return err
		}

		p.Contact = contact
	}

	if err := patch(&p.TaxCode, f.TaxCode, code("tax code", domain.MaxTaxCodeLen)); err != nil {
		return err
	}

	return patch(&p.Note, f.Note, text("note", domain.MaxNoteLen))
}

// Create validates and stores a new partner.
func (uc *PartnerUseCase) Create(ctx context.Context, req CreatePartnerRequest) (*domain.Partner, error) {
	p := &domain.Partner{
		ID:        uuid.NewString(),
		GarageID:  req.GarageID,
		CreatedBy: actorRef(req.ActorID),
		UpdatedBy: actorRef(req.ActorID),
	}

	fields := partnerFields{Type: &req.Type, Name: &req.Name, TaxCode: req.TaxCode, Contact: &req.Contact, Note: req.Note}
	if err := fields.apply(p); err != nil {
		return nil, toAppError("create partner", err)
	}

	if err := uc.partners.Create(ctx, p); err != nil {
		return nil, toAppError("create partner", err)
	}

	return p, nil
}

// Get returns one partner of the garage.
func (uc *PartnerUseCase) Get(ctx context.Context, garageID, partnerID string) (*domain.Partner, error) {
	p, err := uc.partners.FindByID(ctx, garageID, partnerID)
	if err != nil {
		return nil, toAppError("get partner", err)
	}

	return p, nil
}

// Update applies the partial updates in req to a partner.
func (uc *PartnerUseCase) Update(ctx context.Context, req UpdatePartnerRequest) (*domain.Partner, error) {
	p, err := uc.partners.FindByID(ctx, req.GarageID, req.PartnerID)
	if err != nil {
		return nil, toAppError("update partner", err)
	}

	fields := partnerFields{Type: req.Type, Name: req.Name, TaxCode: req.TaxCode, Contact: req.Contact, Note: req.Note}
	if err := fields.apply(p); err != nil {
		return nil, toAppError("update partner", err)
	}

	p.UpdatedBy = actorRef(req.ActorID)

	if err := uc.partners.Update(ctx, p); err != nil {
		return nil, toAppError("update partner", err)
	}

	return p, nil
}

// List returns one page of the garage's partners matching req.
func (uc *PartnerUseCase) List(ctx context.Context, req ListPartnersRequest) (*Page[*domain.Partner], error) {
	if req.Type != "" && !req.Type.IsValid() {
		return nil, toAppError("list partners", domain.ErrInvalidPartnerType)
	}

	page := req.normalize()
	filter := PartnerFilter{ListFilter: page.filter(req.Query), Type: req.Type}

	items, total, err := uc.partners.List(ctx, req.GarageID, filter)
	if err != nil {
		return nil, toAppError("list partners", err)
	}

	return &Page[*domain.Partner]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// Delete soft-deletes a partner.
func (uc *PartnerUseCase) Delete(ctx context.Context, garageID, partnerID, actorID string) error {
	if err := uc.partners.SoftDelete(ctx, garageID, partnerID, actorID); err != nil {
		return toAppError("delete partner", err)
	}

	return nil
}
