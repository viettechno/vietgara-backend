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

	inUse, err := uc.partInUse(ctx, p.GarageID, p.ID)
	if err != nil {
		return err
	}

	if inUse {
		return domain.ErrBaseUnitLocked
	}

	return nil
}

// partInUse reports whether stock, a document or a job refers to the part.
func (uc *UseCase) partInUse(ctx context.Context, garageID, partID string) (bool, error) {
	return lifecycle.AnyInUse(ctx,
		func(ctx context.Context) (bool, error) { return uc.usage.PartInUse(ctx, garageID, partID) },
		func(ctx context.Context) (bool, error) { return uc.repo.PartUsedByJobs(ctx, garageID, partID) },
	)
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
		InUse: func(ctx context.Context) (bool, error) { return uc.partInUse(ctx, req.GarageID, req.ID) },
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

	clean, err := cleanPackaging(p.BaseUnitID, units, func(id string) (*domain.Unit, error) {
		return uc.repo.FindUnit(ctx, garageID, id)
	})
	if err != nil {
		return nil, toAppError("set part units", err)
	}

	if err := uc.barcodesStayValid(p, clean); err != nil {
		return nil, toAppError("set part units", err)
	}

	if err := uc.repo.ReplacePackaging(ctx, garageID, partID, actorID, clean); err != nil {
		return nil, toAppError("set part units", err)
	}

	return uc.GetPart(ctx, garageID, partID)
}

// cleanPackaging validates a packaging table for a part with this base unit:
// the base unit is never one of its own packaging units, a unit appears once,
// every factor is above zero and every unit is active. unitOf loads a unit.
func cleanPackaging(
	baseUnitID string, units []domain.PackagingUnit, unitOf func(id string) (*domain.Unit, error),
) ([]domain.PackagingUnit, error) {
	seen := make(map[string]struct{}, len(units))
	clean := make([]domain.PackagingUnit, 0, len(units))

	for _, u := range units {
		if u.UnitID == baseUnitID {
			return nil, domain.ErrBaseUnitPackaged
		}

		if _, dup := seen[u.UnitID]; dup {
			return nil, domain.ErrDuplicateUnit
		}

		seen[u.UnitID] = struct{}{}

		factor, err := domain.NormalizeFactor(u.Factor)
		if err != nil {
			return nil, err
		}

		if u.SalePrice != nil && *u.SalePrice < 0 {
			return nil, domain.ErrNegativePrice
		}

		unit, err := unitOf(u.UnitID)
		if err != nil {
			return nil, err
		}

		if !unit.IsActive {
			return nil, domain.ErrInactiveUnit
		}

		clean = append(clean, domain.PackagingUnit{UnitID: u.UnitID, Factor: factor, SalePrice: u.SalePrice})
	}

	return clean, nil
}

// cleanBarcodes validates the barcodes of one part: at most MaxPartBarcodes,
// each trimmed and well-formed, none repeated (case-insensitively), and a code
// for a unit names one of the part's packaging units.
func cleanBarcodes(barcodes []domain.PartBarcode, packaging map[string]struct{}) ([]domain.PartBarcode, error) {
	if len(barcodes) > domain.MaxPartBarcodes {
		return nil, domain.ErrBarcodeInvalid
	}

	seen := make(map[string]struct{}, len(barcodes))
	clean := make([]domain.PartBarcode, 0, len(barcodes))

	for _, b := range barcodes {
		code, err := domain.NormalizeBarcode(b.Barcode)
		if err != nil {
			return nil, err
		}

		key := strings.ToLower(code)
		if _, dup := seen[key]; dup {
			return nil, domain.ErrBarcodeAlreadyUsed
		}

		seen[key] = struct{}{}

		if b.UnitID != nil {
			if _, ok := packaging[*b.UnitID]; !ok {
				return nil, domain.ErrUnitNotForPart
			}
		}

		clean = append(clean, domain.PartBarcode{Barcode: code, UnitID: b.UnitID})
	}

	return clean, nil
}

// NewPart is one part created in bulk with its packaging units and barcodes.
type NewPart struct {
	SKU        string
	Name       string
	Brand      string
	CategoryID string
	BaseUnitID string
	Packaging  []domain.PackagingUnit
	Barcodes   []domain.PartBarcode
}

// bulkParts accumulates the rows of CreateParts and loads each unit and
// category once.
type bulkParts struct {
	uc         *UseCase
	ctx        context.Context //nolint:containedctx // scoped to one CreateParts call
	garageID   string
	actorID    string
	units      map[string]*domain.Unit
	categories map[string]struct{}
	batchCodes map[string]struct{}

	parts     []*domain.Part
	packaging []PackagingRow
	barcodes  []BarcodeRow
}

func (b *bulkParts) unitOf(id string) (*domain.Unit, error) {
	if u, ok := b.units[id]; ok {
		return u, nil
	}

	u, err := b.uc.repo.FindUnit(b.ctx, b.garageID, id)
	if err != nil {
		return nil, err
	}

	b.units[id] = u

	return u, nil
}

// add validates one part with the rules of CreatePart, SetPartUnits and
// SetPartBarcodes and queues it for insertion.
func (b *bulkParts) add(n NewPart) error {
	p := &domain.Part{
		ID: uuid.NewString(), GarageID: b.garageID, IsActive: true, ReorderThreshold: "0",
		CreatedBy: actorRef(b.actorID), UpdatedBy: actorRef(b.actorID),
	}

	switch {
	case n.SKU == "":
		return domain.ErrSKURequired
	case n.Name == "":
		return domain.ErrNameRequired
	case n.BaseUnitID == "":
		return domain.ErrBaseUnitRequired
	}

	if err := applyPartText(p, PartFields{SKU: &n.SKU, Name: &n.Name, Brand: &n.Brand}); err != nil {
		return err
	}

	base, err := b.unitOf(n.BaseUnitID)
	if err != nil {
		return err
	}

	if !base.IsActive {
		return domain.ErrInactiveUnit
	}

	p.BaseUnitID = base.ID

	if err := b.category(p, n.CategoryID); err != nil {
		return err
	}

	clean, err := cleanPackaging(p.BaseUnitID, n.Packaging, b.unitOf)
	if err != nil {
		return err
	}

	owned := make(map[string]struct{}, len(clean))

	for _, u := range clean {
		owned[u.UnitID] = struct{}{}
		b.packaging = append(b.packaging, PackagingRow{PartID: p.ID, Unit: u})
	}

	if err := b.codes(p.ID, n.Barcodes, owned); err != nil {
		return err
	}

	b.parts = append(b.parts, p)

	return nil
}

// category points p at the category, checking it the first time it is seen.
func (b *bulkParts) category(p *domain.Part, categoryID string) error {
	if categoryID == "" {
		return nil
	}

	if _, known := b.categories[categoryID]; !known {
		if err := b.uc.applyCategory(b.ctx, p, &categoryID); err != nil {
			return err
		}

		b.categories[categoryID] = struct{}{}
	}

	p.CategoryID = &categoryID

	return nil
}

// codes validates the barcodes of one part and refuses a code used twice in
// the batch.
func (b *bulkParts) codes(partID string, in []domain.PartBarcode, packaging map[string]struct{}) error {
	clean, err := cleanBarcodes(in, packaging)
	if err != nil {
		return err
	}

	for _, code := range clean {
		key := strings.ToLower(code.Barcode)
		if _, dup := b.batchCodes[key]; dup {
			return domain.ErrBarcodeAlreadyUsed
		}

		b.batchCodes[key] = struct{}{}
		b.barcodes = append(b.barcodes, BarcodeRow{PartID: partID, Barcode: code})
	}

	return nil
}

// CreateParts creates parts with their packaging units and barcodes in a few
// statements, for the Excel import (FR-INV-14). Every part obeys the rules of
// CreatePart, SetPartUnits and SetPartBarcodes; units and categories are
// loaded once, and a code used twice in the batch or by another part of the
// garage is refused. It returns the new part ids in input order.
func (uc *UseCase) CreateParts(ctx context.Context, garageID, actorID string, in []NewPart) ([]string, error) {
	const op = "create parts"

	b := &bulkParts{
		uc: uc, ctx: ctx, garageID: garageID, actorID: actorID,
		units: map[string]*domain.Unit{}, categories: map[string]struct{}{}, batchCodes: map[string]struct{}{},
	}

	ids := make([]string, 0, len(in))

	for _, n := range in {
		if err := b.add(n); err != nil {
			return nil, toAppError(op, err)
		}

		ids = append(ids, b.parts[len(b.parts)-1].ID)
	}

	if err := uc.repo.CreateParts(ctx, garageID, actorID, b.parts, b.packaging, b.barcodes); err != nil {
		return nil, toAppError(op, err)
	}

	return ids, nil
}

// barcodesStayValid refuses to remove a packaging unit that has a barcode:
// the code would lose the quantity one scan stands for.
func (uc *UseCase) barcodesStayValid(p *domain.Part, units []domain.PackagingUnit) error {
	if len(p.Barcodes) == 0 {
		return nil
	}

	kept := make(map[string]struct{}, len(units))
	for _, u := range units {
		kept[u.UnitID] = struct{}{}
	}

	for _, b := range p.Barcodes {
		if b.UnitID == nil {
			continue
		}

		if _, ok := kept[*b.UnitID]; !ok {
			return domain.ErrPackagingUnitHasBarcode
		}
	}

	return nil
}

// SetPartBarcodes replaces the part's barcode table (FR-INV-13). Codes are
// trimmed, unique within the request and the garage case-insensitively, and
// a code for a packaging unit must name one of the part's packaging units.
func (uc *UseCase) SetPartBarcodes(
	ctx context.Context, garageID, partID, actorID string, barcodes []domain.PartBarcode,
) (*domain.Part, error) {
	p, err := uc.repo.FindPart(ctx, garageID, partID)
	if err != nil {
		return nil, toAppError("set part barcodes", err)
	}

	packaging := make(map[string]struct{}, len(p.PackagingUnits))
	for _, u := range p.PackagingUnits {
		packaging[u.UnitID] = struct{}{}
	}

	clean, err := cleanBarcodes(barcodes, packaging)
	if err != nil {
		return nil, toAppError("set part barcodes", err)
	}

	if err := uc.repo.ReplaceBarcodes(ctx, garageID, partID, actorID, clean); err != nil {
		return nil, toAppError("set part barcodes", err)
	}

	return uc.GetPart(ctx, garageID, partID)
}

// LookupPartByBarcode finds the part a scanned code belongs to (FR-INV-13),
// including inactive parts so the caller can say so, and returns the matched
// code with the unit one scan stands for.
func (uc *UseCase) LookupPartByBarcode(ctx context.Context, garageID, barcode string) (*domain.Part, *domain.PartBarcode, error) {
	// A scan is only trimmed and length-checked: a code with characters the
	// garage cannot store simply is not found.
	code := strings.TrimSpace(barcode)
	if code == "" || len([]rune(code)) > domain.MaxBarcodeLen {
		return nil, nil, toAppError("lookup barcode", domain.ErrBarcodeInvalid)
	}

	p, matched, err := uc.repo.FindBarcode(ctx, garageID, code)
	if err != nil {
		return nil, nil, toAppError("lookup barcode", err)
	}

	if err := uc.fillImages(ctx, garageID, []*domain.Part{p}); err != nil {
		return nil, nil, toAppError("lookup barcode", err)
	}

	return p, matched, nil
}

// PartsBySKUs returns the parts with the given SKUs, matched
// case-insensitively, for the Excel import.
func (uc *UseCase) PartsBySKUs(ctx context.Context, garageID string, skus []string) ([]*domain.Part, error) {
	if len(skus) == 0 {
		return nil, nil
	}

	parts, err := uc.repo.FindPartsBySKUs(ctx, garageID, skus)
	if err != nil {
		return nil, toAppError("find parts by SKU", err)
	}

	return parts, nil
}

// PartsByBarcodes returns the parts (with their barcodes) that own any of
// the codes, for the Excel import of receipts and counts.
func (uc *UseCase) PartsByBarcodes(ctx context.Context, garageID string, codes []string) ([]*domain.Part, error) {
	owners, err := uc.repo.BarcodesByCode(ctx, garageID, codes)
	if err != nil {
		return nil, toAppError("find parts by barcode", err)
	}

	if len(owners) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(owners))
	ids := make([]string, 0, len(owners))

	for _, partID := range owners {
		if _, dup := seen[partID]; !dup {
			seen[partID] = struct{}{}
			ids = append(ids, partID)
		}
	}

	parts, err := uc.repo.FindPartsByIDs(ctx, garageID, ids)
	if err != nil {
		return nil, toAppError("find parts by barcode", err)
	}

	return parts, nil
}

// BarcodesByCode returns the part id per lower-case code for the codes that
// exist in the garage, for the Excel import's uniqueness check.
func (uc *UseCase) BarcodesByCode(ctx context.Context, garageID string, codes []string) (map[string]string, error) {
	out, err := uc.repo.BarcodesByCode(ctx, garageID, codes)
	if err != nil {
		return nil, toAppError("find barcodes", err)
	}

	return out, nil
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
