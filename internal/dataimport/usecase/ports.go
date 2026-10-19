package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// SheetRow is one non-empty row read from the workbook; Number is the row as
// the sheet shows it.
type SheetRow struct {
	Number int
	Cells  []string
}

// Sheet is the first sheet of an uploaded workbook.
type Sheet struct {
	Rows []SheetRow
}

// Reader parses an .xlsx payload. The adapter lives in cmd/server (see
// internal/dataimport/xlsx).
type Reader interface {
	Read(content []byte) (*Sheet, error)
}

// PackagingUnit is one non-base unit of a part.
type PackagingUnit struct {
	UnitID string
	Factor string
}

// PartInfo is what Data Import needs to know about a part.
type PartInfo struct {
	ID             string
	SKU            string
	Name           string
	IsActive       bool
	BaseUnitID     string
	PackagingUnits []PackagingUnit
}

// UnitInfo is a garage unit of measure.
type UnitInfo struct {
	ID           string
	Name         string
	Abbreviation string
	IsActive     bool
}

// CategoryInfo is a part category.
type CategoryInfo struct {
	ID   string
	Name string
}

// NewPart is one part to create with its packaging units and barcodes; the
// barcodes name their unit by id.
type NewPart struct {
	SKU        string
	Name       string
	Brand      string
	CategoryID string
	BaseUnitID string
	Packaging  []PackagingUnit
	Barcodes   []BarcodeInput
}

// BarcodeInput is one code to attach to a part.
type BarcodeInput struct {
	Barcode string
	UnitID  *string
}

// BarcodeMatch is the part a scanned code belongs to and the unit one scan
// stands for (empty for the base unit).
type BarcodeMatch struct {
	Part   PartInfo
	UnitID string
}

// Catalogue is the Data Import module's view of the Catalogue, implemented
// in cmd/server over the Catalogue use case. It lets the module create parts,
// units and categories exactly as the screens do.
type Catalogue interface {
	PartsBySKUs(ctx context.Context, garageID string, skus []string) ([]PartInfo, error)
	Units(ctx context.Context, garageID string) ([]UnitInfo, error)
	Categories(ctx context.Context, garageID string) ([]CategoryInfo, error)
	CreateUnit(ctx context.Context, garageID, actorID, name, abbreviation string) (UnitInfo, error)
	CreateCategory(ctx context.Context, garageID, actorID, name string) (CategoryInfo, error)
	// CreateParts creates the parts, their packaging units and barcodes in
	// bulk and returns their ids in input order.
	CreateParts(ctx context.Context, garageID, actorID string, parts []NewPart) ([]string, error)
	// PartsByBarcodes returns the match per lower-case code for the codes that
	// belong to a part of the garage.
	PartsByBarcodes(ctx context.Context, garageID string, codes []string) (map[string]BarcodeMatch, error)
	// ExistingBarcodes returns the part id per lower-case code.
	ExistingBarcodes(ctx context.Context, garageID string, codes []string) (map[string]string, error)
}

// Suppliers resolves supplier names to ids; only existing suppliers are
// accepted (an unknown name is a row error, never a new supplier).
type Suppliers interface {
	IDsByName(ctx context.Context, garageID string, names []string) (map[string]string, error)
}

// ReceiptLine is one line of an imported goods receipt.
type ReceiptLine struct {
	PartID     string
	UnitID     string
	Quantity   string
	UnitCost   int64
	SupplierID string
}

// ReceiptInput is an imported goods receipt.
type ReceiptInput struct {
	ReceiptDate  string
	DeliveryNote string
	Notes        string
	Lines        []ReceiptLine
}

// CountLine is one counted line of an imported stock count.
type CountLine struct {
	PartID   string
	UnitID   string
	Quantity string
}

// CountInput is an imported stock count.
type CountInput struct {
	Name  string
	Notes string
	Lines []CountLine
}

// Document is the document an import created.
type Document struct {
	ID     string
	Number string
}

// Inventory is the Data Import module's view of Inventory, implemented in
// cmd/server over the Inventory use case.
type Inventory interface {
	// CreateGoodsReceipt saves the receipt and, when complete is true, posts
	// it in the same transaction (the opening stock of a parts import).
	CreateGoodsReceipt(ctx context.Context, garageID, actorID string, canSetCost bool, in ReceiptInput, complete bool) (Document, error)
	CreateStockCount(ctx context.Context, garageID, actorID string, in CountInput) (Document, error)
}

// AuditLogger records the confirmed import in the caller's transaction, so an
// entry exists exactly when the import was committed.
type AuditLogger interface {
	Record(ctx context.Context, e audit.Entry) error
}

// Run is one confirmed import (the import_runs row).
type Run struct {
	ID         string
	GarageID   string
	Type       string
	FileSHA256 string
	FileName   string
	RowCount   int
	DocumentID *string
	CreatedBy  *string
}

// RunRepository guards idempotency and keeps the import history.
type RunRepository interface {
	// Find returns the run for the same garage, type and file hash, or
	// (nil, nil).
	Find(ctx context.Context, garageID, importType, sha256 string) (*Run, error)
	// Create inserts the run; a concurrent duplicate returns
	// domain.ErrFileAlreadyImported.
	Create(ctx context.Context, run *Run) error
}
