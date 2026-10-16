package usecase

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// PartFields are the settable fields of a part; nil fields are left
// unchanged (on create: defaulted). CategoryID "" clears the category.
type PartFields struct {
	SKU              *string
	Name             *string
	BaseUnitID       *string
	SalePrice        *int64
	CategoryID       *string
	Brand            *string
	IsActive         *bool
	ReorderThreshold *string
}

// CreatePartRequest is the input for UseCase.CreatePart.
type CreatePartRequest struct {
	GarageID string
	ActorID  string
	Fields   PartFields
}

// UpdatePartRequest is the input for UseCase.UpdatePart.
type UpdatePartRequest struct {
	GarageID string
	PartID   string
	ActorID  string
	Fields   PartFields
}

// ListPartsRequest is the input for UseCase.ListParts.
type ListPartsRequest struct {
	PageRequest

	GarageID string
	// Query matches SKU, name or brand.
	Query string
	// CategoryID also matches the sub-categories.
	CategoryID      string
	Brand           string
	IncludeInactive bool
}

// CreatePart adds a part to the garage's catalogue. The base unit is
// required: every stored quantity is expressed in it.
func (uc *UseCase) CreatePart(ctx context.Context, req CreatePartRequest) (*domain.Part, error) {
	p := &domain.Part{
		ID: uuid.NewString(), GarageID: req.GarageID, IsActive: true, ReorderThreshold: "0",
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	switch {
	case req.Fields.SKU == nil:
		return nil, toAppError("create part", domain.ErrSKURequired)
	case req.Fields.Name == nil:
		return nil, toAppError("create part", domain.ErrNameRequired)
	case req.Fields.BaseUnitID == nil || *req.Fields.BaseUnitID == "":
		return nil, toAppError("create part", domain.ErrBaseUnitRequired)
	}

	if err := uc.applyPartFields(ctx, p, req.Fields); err != nil {
		return nil, toAppError("create part", err)
	}

	if err := uc.repo.CreatePart(ctx, p); err != nil {
		return nil, toAppError("create part", err)
	}

	return uc.GetPart(ctx, req.GarageID, p.ID)
}

// GetPart returns one part of the garage with its base unit, category,
// packaging units and images.
func (uc *UseCase) GetPart(ctx context.Context, garageID, partID string) (*domain.Part, error) {
	p, err := uc.repo.FindPart(ctx, garageID, partID)
	if err != nil {
		return nil, toAppError("get part", err)
	}

	if err := uc.fillImages(ctx, garageID, []*domain.Part{p}); err != nil {
		return nil, toAppError("get part", err)
	}

	return p, nil
}

// UpdatePart applies the partial updates in req to a part. The base unit
// can change only while the part has no packaging units and nothing uses it
// (stock movements, document lines): otherwise every stored quantity would
// silently change meaning.
func (uc *UseCase) UpdatePart(ctx context.Context, req UpdatePartRequest) (*domain.Part, error) {
	p, err := uc.repo.FindPart(ctx, req.GarageID, req.PartID)
	if err != nil {
		return nil, toAppError("update part", err)
	}

	if f := req.Fields; f.BaseUnitID != nil && *f.BaseUnitID != p.BaseUnitID {
		if err := uc.checkBaseUnitChange(ctx, p); err != nil {
			return nil, toAppError("update part", err)
		}
	}

	if err := uc.applyPartFields(ctx, p, req.Fields); err != nil {
		return nil, toAppError("update part", err)
	}

	p.UpdatedBy = actorRef(req.ActorID)

	if err := uc.repo.UpdatePart(ctx, p); err != nil {
		return nil, toAppError("update part", err)
	}

	return uc.GetPart(ctx, req.GarageID, req.PartID)
}

func (uc *UseCase) checkBaseUnitChange(ctx context.Context, p *domain.Part) error {
	if len(p.PackagingUnits) > 0 {
		return domain.ErrBaseUnitLocked
	}

	inUse, err := uc.usage.PartInUse(ctx, p.GarageID, p.ID)
	if err != nil {
		return err
	}

	if inUse {
		return domain.ErrBaseUnitLocked
	}

	return nil
}

// ListParts returns one page of the garage's parts.
func (uc *UseCase) ListParts(ctx context.Context, req ListPartsRequest) (*Page[*domain.Part], error) {
	page := req.normalize()
	filter := PartFilter{
		Query: strings.TrimSpace(req.Query), Brand: strings.TrimSpace(req.Brand), IncludeInactive: req.IncludeInactive,
		Limit: page.PageSize, Offset: page.offset(),
	}

	if req.CategoryID != "" {
		all, err := uc.repo.ListCategories(ctx, req.GarageID, true)
		if err != nil {
			return nil, toAppError("list parts", err)
		}

		filter.CategoryIDs = descendantIDs(all, req.CategoryID)
	}

	parts, total, err := uc.repo.ListParts(ctx, req.GarageID, filter)
	if err != nil {
		return nil, toAppError("list parts", err)
	}

	if err := uc.fillImages(ctx, req.GarageID, parts); err != nil {
		return nil, toAppError("list parts", err)
	}

	return &Page[*domain.Part]{Items: parts, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// PartsByIDs returns the parts with the given ids, for other modules that
// join them with their own data (the stock-on-hand list, the movement
// report). Unknown ids are skipped.
func (uc *UseCase) PartsByIDs(ctx context.Context, garageID string, ids []string) ([]*domain.Part, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	parts, err := uc.repo.FindPartsByIDs(ctx, garageID, ids)
	if err != nil {
		return nil, toAppError("find parts", err)
	}

	if err := uc.fillImages(ctx, garageID, parts); err != nil {
		return nil, toAppError("find parts", err)
	}

	return parts, nil
}

// DeletePart follows the delete-or-deactivate rule (FR-CAT-07): a part that
// stock movements or documents refer to is deactivated, any other is
// soft-deleted (which frees its SKU).
func (uc *UseCase) DeletePart(ctx context.Context, req DeleteRequest) (lifecycle.Outcome, error) {
	if _, err := uc.repo.FindPart(ctx, req.GarageID, req.ID); err != nil {
		return "", toAppError("delete part", err)
	}

	outcome, err := lifecycle.Apply(ctx, req.DryRun, lifecycle.Rule{
		InUse: func(ctx context.Context) (bool, error) { return uc.usage.PartInUse(ctx, req.GarageID, req.ID) },
		Delete: func(ctx context.Context) error {
			return uc.repo.SoftDeletePart(ctx, req.GarageID, req.ID, req.ActorID)
		},
		Deactivate: func(ctx context.Context) error {
			return uc.repo.SetPartActive(ctx, req.GarageID, req.ID, false, req.ActorID)
		},
	})
	if err != nil {
		return "", toAppError("delete part", err)
	}

	return outcome, nil
}

// ActivatePart reactivates a deactivated part.
func (uc *UseCase) ActivatePart(ctx context.Context, garageID, partID, actorID string) (*domain.Part, error) {
	if err := uc.repo.SetPartActive(ctx, garageID, partID, true, actorID); err != nil {
		return nil, toAppError("activate part", err)
	}

	return uc.GetPart(ctx, garageID, partID)
}

// SetPartUnits replaces the part's packaging table (FR-CAT-02). The base unit
// is never one of its own packaging units, a unit appears once, every factor
// is above zero and every unit is active.
func (uc *UseCase) SetPartUnits(ctx context.Context, garageID, partID, actorID string, units []domain.PackagingUnit) (*domain.Part, error) {
	p, err := uc.repo.FindPart(ctx, garageID, partID)
	if err != nil {
		return nil, toAppError("set part units", err)
	}

	seen := make(map[string]struct{}, len(units))
	clean := make([]domain.PackagingUnit, 0, len(units))

	for _, u := range units {
		if u.UnitID == p.BaseUnitID {
			return nil, toAppError("set part units", domain.ErrBaseUnitPackaged)
		}

		if _, dup := seen[u.UnitID]; dup {
			return nil, toAppError("set part units", domain.ErrDuplicateUnit)
		}

		seen[u.UnitID] = struct{}{}

		factor, err := domain.NormalizeFactor(u.Factor)
		if err != nil {
			return nil, toAppError("set part units", err)
		}

		if u.SalePrice != nil && *u.SalePrice < 0 {
			return nil, toAppError("set part units", domain.ErrNegativePrice)
		}

		unit, err := uc.repo.FindUnit(ctx, garageID, u.UnitID)
		if err != nil {
			return nil, toAppError("set part units", err)
		}

		if !unit.IsActive {
			return nil, toAppError("set part units", domain.ErrInactiveUnit)
		}

		clean = append(clean, domain.PackagingUnit{UnitID: u.UnitID, Factor: factor, SalePrice: u.SalePrice})
	}

	if err := uc.repo.ReplacePackaging(ctx, garageID, partID, actorID, clean); err != nil {
		return nil, toAppError("set part units", err)
	}

	return uc.GetPart(ctx, garageID, partID)
}

// AddPartImage attaches an uploaded part image, at most three per part.
func (uc *UseCase) AddPartImage(ctx context.Context, garageID, partID, fileID, actorID string) (*domain.Part, error) {
	if _, err := uc.repo.FindPart(ctx, garageID, partID); err != nil {
		return nil, toAppError("add part image", err)
	}

	images, err := uc.repo.ListImages(ctx, garageID, partID)
	if err != nil {
		return nil, toAppError("add part image", err)
	}

	if len(images) >= domain.MaxPartImages {
		return nil, toAppError("add part image", domain.ErrTooManyImages)
	}

	if err := uc.files.PartImage(ctx, garageID, partID, fileID); err != nil {
		return nil, toAppError("add part image", err)
	}

	attached, err := uc.repo.FileAttached(ctx, garageID, fileID)
	if err != nil {
		return nil, toAppError("add part image", err)
	}

	if attached {
		return nil, toAppError("add part image", domain.ErrImageAlreadyAttached)
	}

	if err := uc.repo.AddImage(ctx, garageID, partID, fileID, actorID); err != nil {
		return nil, toAppError("add part image", err)
	}

	return uc.GetPart(ctx, garageID, partID)
}

// RemovePartImage detaches an image and retires its file.
func (uc *UseCase) RemovePartImage(ctx context.Context, garageID, partID, fileID, actorID string) (*domain.Part, error) {
	if err := uc.repo.RemoveImage(ctx, garageID, partID, fileID); err != nil {
		return nil, toAppError("remove part image", err)
	}

	if err := uc.files.Release(ctx, garageID, fileID, actorID); err != nil {
		return nil, toAppError("remove part image", err)
	}

	return uc.GetPart(ctx, garageID, partID)
}

// fillImages sets each part's images (display order, with URLs).
func (uc *UseCase) fillImages(ctx context.Context, garageID string, parts []*domain.Part) error {
	var ids []string

	for _, p := range parts {
		for _, img := range p.Images {
			ids = append(ids, img.FileID)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	urls, err := uc.files.URLs(ctx, garageID, ids)
	if err != nil {
		return err
	}

	for _, p := range parts {
		for i := range p.Images {
			p.Images[i].URL = urls[p.Images[i].FileID]
		}
	}

	return nil
}

func (uc *UseCase) applyPartFields(ctx context.Context, p *domain.Part, f PartFields) error {
	if err := applyPartText(p, f); err != nil {
		return err
	}

	if err := uc.applyBaseUnit(ctx, p, f.BaseUnitID); err != nil {
		return err
	}

	if f.SalePrice != nil {
		if *f.SalePrice < 0 {
			return domain.ErrNegativePrice
		}

		p.SalePrice = *f.SalePrice
	}

	if err := uc.applyCategory(ctx, p, f.CategoryID); err != nil {
		return err
	}

	if f.IsActive != nil {
		p.IsActive = *f.IsActive
	}

	if f.ReorderThreshold != nil {
		threshold, err := domain.NormalizeThreshold(*f.ReorderThreshold)
		if err != nil {
			return err
		}

		p.ReorderThreshold = threshold
	}

	return nil
}

// applyPartText applies the SKU, the name and the brand.
func applyPartText(p *domain.Part, f PartFields) error {
	if f.SKU != nil {
		sku := strings.TrimSpace(*f.SKU)
		if sku == "" {
			return domain.ErrSKURequired
		}

		if err := domain.CheckLength(sku, domain.MaxSKULen); err != nil {
			return err
		}

		p.SKU = sku
	}

	if f.Name != nil {
		name := strings.TrimSpace(*f.Name)
		if name == "" {
			return domain.ErrNameRequired
		}

		if err := domain.CheckLength(name, domain.MaxNameLen); err != nil {
			return err
		}

		p.Name = name
	}

	if f.Brand != nil {
		brand := strings.TrimSpace(*f.Brand)
		if err := domain.CheckLength(brand, domain.MaxBrandLen); err != nil {
			return err
		}

		p.Brand = brand
	}

	return nil
}

// applyBaseUnit points the part at an active unit when it is not already its
// base unit.
func (uc *UseCase) applyBaseUnit(ctx context.Context, p *domain.Part, unitID *string) error {
	if unitID == nil || (p.BaseUnitID != "" && *unitID == p.BaseUnitID) {
		return nil
	}

	unit, err := uc.repo.FindUnit(ctx, p.GarageID, *unitID)
	if err != nil {
		return err
	}

	if !unit.IsActive {
		return domain.ErrInactiveUnit
	}

	p.BaseUnitID, p.BaseUnitName, p.BaseUnitAbbreviation = unit.ID, unit.Name, unit.Abbreviation

	return nil
}

func (uc *UseCase) applyCategory(ctx context.Context, p *domain.Part, categoryID *string) error {
	if categoryID == nil {
		return nil
	}

	if *categoryID == "" {
		p.CategoryID, p.CategoryName = nil, ""

		return nil
	}

	if p.CategoryID != nil && *p.CategoryID == *categoryID {
		return nil
	}

	c, err := uc.repo.FindCategory(ctx, p.GarageID, *categoryID)
	if err != nil {
		return err
	}

	if !c.IsActive {
		return domain.ErrInactiveCategory
	}

	id := c.ID
	p.CategoryID, p.CategoryName = &id, c.Name

	return nil
}
