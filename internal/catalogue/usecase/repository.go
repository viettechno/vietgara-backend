// Package usecase contains the application use cases of the Catalogue module
// (FRD 3.21): parts and their price, brand and category (FR-CAT-03, FR-CAT-09),
// units of measure (FR-CAT-01), packaging and the conversion of a quantity to
// the base unit (FR-CAT-02), the category tree (FR-CAT-03) and part images
// (FR-CAT-04). Every method expects to run inside a garage-scoped transaction
// (see middleware.Garage) so Row-Level Security applies. Catalogue depends on
// no other module: what only others can answer (is a part used by stock or a
// document?) goes through the ports below, wired in cmd/server.
package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
)

// PartFilter selects one page of a garage's parts.
type PartFilter struct {
	// Query matches SKU, name or brand.
	Query string
	// CategoryIDs restricts to these categories (a category and its
	// descendants); empty means any.
	CategoryIDs     []string
	Brand           string
	IncludeInactive bool
	Limit           int
	Offset          int
}

// UnitFilter selects one page of a garage's units.
type UnitFilter struct {
	IncludeInactive bool
	Limit           int
	Offset          int
}

// JobFilter selects one page of a garage's jobs.
type JobFilter struct {
	// Query matches the name or the category.
	Query           string
	Category        string
	IncludeInactive bool
	Limit           int
	Offset          int
}

// ImageRef is one stored image link of a part.
type ImageRef struct {
	FileID   string
	Position int
}

// PackagingRow is one packaging unit of a part created in bulk.
type PackagingRow struct {
	PartID string
	Unit   domain.PackagingUnit
}

// BarcodeRow is one barcode of a part created in bulk.
type BarcodeRow struct {
	PartID  string
	Barcode domain.PartBarcode
}

// Repository defines the persistence contract of the module. Every method is
// scoped to one garage and ignores soft-deleted rows.
type Repository interface {
	CreateUnit(ctx context.Context, u *domain.Unit) error
	FindUnit(ctx context.Context, garageID, unitID string) (*domain.Unit, error)
	UpdateUnit(ctx context.Context, u *domain.Unit) error
	ListUnits(ctx context.Context, garageID string, filter UnitFilter) ([]*domain.Unit, int, error)
	SoftDeleteUnit(ctx context.Context, garageID, unitID, actorID string) error
	SetUnitActive(ctx context.Context, garageID, unitID string, active bool, actorID string) error
	// UnitUsedByParts reports whether a live part has the unit as its base
	// unit or as a packaging unit.
	UnitUsedByParts(ctx context.Context, garageID, unitID string) (bool, error)

	CreateCategory(ctx context.Context, c *domain.Category) error
	FindCategory(ctx context.Context, garageID, categoryID string) (*domain.Category, error)
	UpdateCategory(ctx context.Context, c *domain.Category) error
	// ListCategories returns the whole tree as a flat list ordered by name.
	ListCategories(ctx context.Context, garageID string, includeInactive bool) ([]*domain.Category, error)
	SoftDeleteCategory(ctx context.Context, garageID, categoryID, actorID string) error
	SetCategoryActive(ctx context.Context, garageID, categoryID string, active bool, actorID string) error
	// CategoryHasParts reports whether a live part is in the category.
	CategoryHasParts(ctx context.Context, garageID, categoryID string) (bool, error)

	CreatePart(ctx context.Context, p *domain.Part) error
	// CreateParts inserts the parts with their packaging units and barcodes in
	// bulk. A SKU or a barcode already taken returns the matching domain error.
	CreateParts(ctx context.Context, garageID, actorID string, parts []*domain.Part, packaging []PackagingRow, barcodes []BarcodeRow) error
	// FindPart returns the part with its base unit, category, packaging
	// units, barcodes and image links, or domain.ErrPartNotFound.
	FindPart(ctx context.Context, garageID, partID string) (*domain.Part, error)
	FindPartsByIDs(ctx context.Context, garageID string, ids []string) ([]*domain.Part, error)
	// FindPartsBySKUs returns the live and inactive parts with these SKUs,
	// matched case-insensitively.
	FindPartsBySKUs(ctx context.Context, garageID string, skus []string) ([]*domain.Part, error)
	UpdatePart(ctx context.Context, p *domain.Part) error
	ListParts(ctx context.Context, garageID string, filter PartFilter) ([]*domain.Part, int, error)
	SoftDeletePart(ctx context.Context, garageID, partID, actorID string) error
	SetPartActive(ctx context.Context, garageID, partID string, active bool, actorID string) error
	// ReplacePackaging replaces the part's packaging table.
	ReplacePackaging(ctx context.Context, garageID, partID, actorID string, units []domain.PackagingUnit) error
	// ReplaceBarcodes replaces the part's barcode table. A code already used
	// by another part returns domain.ErrBarcodeAlreadyUsed.
	ReplaceBarcodes(ctx context.Context, garageID, partID, actorID string, barcodes []domain.PartBarcode) error
	// FindBarcode returns the part and the barcode row that matches code
	// case-insensitively, or domain.ErrBarcodeNotFound.
	FindBarcode(ctx context.Context, garageID, barcode string) (*domain.Part, *domain.PartBarcode, error)
	// BarcodesByCode returns the part id per lower-case code for the codes
	// that exist in the garage.
	BarcodesByCode(ctx context.Context, garageID string, codes []string) (map[string]string, error)

	CreateJob(ctx context.Context, j *domain.Job) error
	// FindJob returns the live job with its suggested parts (ids, amounts and
	// units only), or domain.ErrJobNotFound.
	FindJob(ctx context.Context, garageID, jobID string) (*domain.Job, error)
	// UpdateJob persists the job's fields and, when replaceParts is true, its
	// suggested parts.
	UpdateJob(ctx context.Context, j *domain.Job, replaceParts bool) error
	ListJobs(ctx context.Context, garageID string, filter JobFilter) ([]*domain.Job, int, error)
	SoftDeleteJob(ctx context.Context, garageID, jobID, actorID string) error
	SetJobActive(ctx context.Context, garageID, jobID string, active bool, actorID string) error
	// PartUsedByJobs reports whether a live job suggests the part.
	PartUsedByJobs(ctx context.Context, garageID, partID string) (bool, error)
	ListImages(ctx context.Context, garageID, partID string) ([]ImageRef, error)
	AddImage(ctx context.Context, garageID, partID, fileID, actorID string) error
	RemoveImage(ctx context.Context, garageID, partID, fileID string) error
	// FileAttached reports whether any part uses the file.
	FileAttached(ctx context.Context, garageID, fileID string) (bool, error)
}

// UsageChecker answers what only other modules can: whether stock movements
// or documents refer to a part or a unit. It decides between deleting and
// deactivating (FR-CAT-07) and freezes a part's base unit. It is implemented
// in cmd/server over Inventory, Quote and Repair Order, so Catalogue imports
// none of them.
type UsageChecker interface {
	PartInUse(ctx context.Context, garageID, partID string) (bool, error)
	UnitInUse(ctx context.Context, garageID, unitID string) (bool, error)
	// JobInUse reports whether a quote line came from the job.
	JobInUse(ctx context.Context, garageID, jobID string) (bool, error)
}

// Files is the Catalogue's view of the File module, implemented in
// cmd/server.
type Files interface {
	// PartImage checks that the file is an active part_image of the garage
	// uploaded for this part.
	PartImage(ctx context.Context, garageID, partID, fileID string) error
	// URLs returns the public URL of each file id it knows.
	URLs(ctx context.Context, garageID string, fileIDs []string) (map[string]string, error)
	// Release retires a file no part uses any more (delete or deactivate).
	Release(ctx context.Context, garageID, fileID, actorID string) error
}
