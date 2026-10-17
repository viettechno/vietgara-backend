// Package usecase contains the application use cases of the Inventory
// module (FRD 3.7): the stock on hand of the Catalogue's parts (FR-INV-03),
// goods receipt documents that post stock and the weighted-average cost
// (FR-INV-06/07), the usual suppliers of a part (FR-INV-19), the stock issue
// document of each repair order, drafted when the order is created and posted
// when it starts or closes (FR-INV-02/20), the stock movement report
// (FR-INV-05) and the purchases and valuation reports (FR-INV-07/19). Every
// method expects to run inside a garage-scoped transaction (see
// middleware.Garage) so Row-Level Security applies. What a part is, and how a
// quantity typed in a packaging unit becomes a base-unit quantity, is the
// Catalogue's business and reached through PartCatalogue. See:
// Coding_Standards.docx Section 2.3, HLD Section 4.
package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// TransactionFilter selects one page of a garage's stock transactions
// (FR-INV-05).
type TransactionFilter struct {
	PartID string
	Type   domain.TransactionType
	Limit  int
	Offset int
}

// Balance is the stock position of one part, in the base unit.
type Balance struct {
	// OnHand is the quantity on hand; Reserved is held by the part's draft
	// stock issues. Both are decimal strings.
	OnHand   string
	Reserved string
	// AverageCost is the weighted-average cost per base unit, VND with up to
	// four decimals.
	AverageCost string
}

// GoodsReceiptFilter selects one page of a garage's goods receipts.
type GoodsReceiptFilter struct {
	// Query matches the number, delivery note number or notes.
	Query string
	// SupplierID keeps receipts with a line from this supplier.
	SupplierID string
	Status     domain.GoodsReceiptStatus
	Limit      int
	Offset     int
}

// PurchaseFilter selects the completed receipt lines of a purchases report
// (FR-INV-19): From inclusive, To exclusive, both "YYYY-MM-DD" or empty.
type PurchaseFilter struct {
	SupplierID string
	From       string
	To         string
}

// ValuationRow is one part's position after the last movement at or before a
// date; the use case joins it with the Catalogue's description of the part.
type ValuationRow struct {
	PartID      string
	Quantity    string
	AverageCost string
}

// Repository defines the persistence contract for stock balances, stock
// movements and the goods receipt and stock issue documents. Every method is
// scoped to the given garage.
type Repository interface {
	// Balances returns the stock position per part id; a part without
	// movements is absent. With ids empty it returns every part of the garage
	// that has a balance.
	Balances(ctx context.Context, garageID string, ids []string) (map[string]Balance, error)

	// RecordTransaction inserts tx and atomically applies its effect to
	// stock_balance in the same statement group: +quantity for
	// TransactionIn, -quantity for TransactionOut. An IN with a UnitCost moves
	// the weighted-average cost; the OUT that reverses a goods receipt line
	// (RefGoodsReceipt with a UnitCost) unwinds it. Any other OUT leaves the
	// average alone and, when tx has no UnitCost, is stamped with it.
	RecordTransaction(ctx context.Context, tx *domain.StockTransaction) error
	// ListTransactions returns one page of transactions (FR-INV-05),
	// newest first, without the derived part and unit descriptions.
	ListTransactions(ctx context.Context, garageID string, filter TransactionFilter) ([]*domain.StockTransaction, int, error)

	// CreateGoodsReceipt inserts the receipt header and its lines.
	CreateGoodsReceipt(ctx context.Context, r *domain.GoodsReceipt) error
	// FindGoodsReceipt returns the receipt with its lines, or
	// domain.ErrGoodsReceiptNotFound. forUpdate locks the row until the
	// transaction ends, so two completions of one receipt serialize.
	FindGoodsReceipt(ctx context.Context, garageID, receiptID string, forUpdate bool) (*domain.GoodsReceipt, error)
	// SaveGoodsReceipt persists the header, status and timestamps of r and
	// replaces its lines.
	SaveGoodsReceipt(ctx context.Context, r *domain.GoodsReceipt) error
	// ListGoodsReceipts returns one page of receipts, newest first, without
	// lines but with TotalAmount.
	ListGoodsReceipts(ctx context.Context, garageID string, filter GoodsReceiptFilter) ([]*domain.GoodsReceipt, int, error)

	// PartSuppliers returns the usual suppliers of a part, default first,
	// without the derived supplier names.
	PartSuppliers(ctx context.Context, garageID, partID string) ([]domain.PartSupplier, error)
	// ReplacePartSuppliers replaces the part's list as a whole; the last
	// cost of a supplier that stays on the list is kept.
	ReplacePartSuppliers(ctx context.Context, garageID, partID string, list []domain.PartSupplier) error
	// DefaultSuppliers returns the default supplier id per part id.
	DefaultSuppliers(ctx context.Context, garageID string, partIDs []string) (map[string]string, error)
	// RecordLastCost sets the last cost paid (per base unit, a decimal
	// string) for the part and supplier, adding the supplier to the part's
	// list when it is not there yet.
	RecordLastCost(ctx context.Context, garageID, partID, supplierID, unitCost string) error

	// CreateStockIssue inserts the issue and its lines; an issue that already
	// exists for the repair order returns that one's id unchanged: created is
	// false.
	CreateStockIssue(ctx context.Context, issue *domain.StockIssue) (created bool, err error)
	// FindStockIssue returns the issue with its lines, or
	// domain.ErrStockIssueNotFound. forUpdate locks it.
	FindStockIssue(ctx context.Context, garageID, issueID string, forUpdate bool) (*domain.StockIssue, error)
	// FindStockIssueByRepairOrder is FindStockIssue by the repair order.
	FindStockIssueByRepairOrder(ctx context.Context, garageID, repairOrderID string, forUpdate bool) (*domain.StockIssue, error)
	// SaveStockIssue persists the status, posting and line costs.
	SaveStockIssue(ctx context.Context, issue *domain.StockIssue) error

	// PurchasesReport returns the lines of completed receipts, newest first.
	PurchasesReport(ctx context.Context, garageID string, filter PurchaseFilter) ([]domain.PurchaseLine, error)
	// StockValuation returns every part's position after its last movement
	// before upTo (exclusive).
	StockValuation(ctx context.Context, garageID string, upTo time.Time) ([]ValuationRow, error)
}

// PartInfo is the Catalogue's description of a part, as Inventory needs it.
type PartInfo struct {
	ID                   string
	SKU                  string
	Name                 string
	BaseUnitName         string
	BaseUnitAbbreviation string
	CategoryID           string
	CategoryName         string
	Brand                string
	IsActive             bool
	// ReorderThreshold is a decimal string in the base unit.
	ReorderThreshold string
	// ThumbnailURL is the first image of the part, "" without images.
	ThumbnailURL string
}

// PartQuery selects the parts of a stock list from the Catalogue.
type PartQuery struct {
	// Query matches SKU, name or brand.
	Query string
	// CategoryID also matches the sub-categories.
	CategoryID      string
	Brand           string
	IncludeInactive bool
	Limit           int
	Offset          int
}

// ResolvedQuantity is a quantity converted to the base unit of its part.
type ResolvedQuantity struct {
	// Base is the amount in the base unit.
	Base string
	// UnitID and Entered are set when a packaging unit was used.
	UnitID  *string
	Entered *string
}

// PartCatalogue is the Inventory's view of the Catalogue module,
// implemented in cmd/server so Inventory imports no other module.
type PartCatalogue interface {
	// ResolveQuantity converts entered, typed in unitID (empty for the
	// base unit), to the base unit of the part. It errors for an unknown
	// part, a unit that is not one of the part's units, or a result that
	// needs more than three decimals.
	ResolveQuantity(ctx context.Context, garageID, partID, unitID, entered string) (ResolvedQuantity, error)
	// Parts returns the parts with these ids (deactivated ones too);
	// unknown ids are skipped.
	Parts(ctx context.Context, garageID string, ids []string) ([]PartInfo, error)
	// SearchParts returns one page of parts, ordered by name, and the total.
	SearchParts(ctx context.Context, garageID string, q PartQuery) ([]PartInfo, int, error)
	// UnitAbbreviations returns the abbreviation per unit id.
	UnitAbbreviations(ctx context.Context, garageID string, ids []string) (map[string]string, error)
}

// GarageSettings is the Tenant module's view of a garage as Inventory needs
// it: the time zone and the numbering and posting configuration.
type GarageSettings struct {
	Timezone                   string
	GoodsReceiptNumberTemplate string
	StockIssueNumberTemplate   string
	// StockIssueMode is "on_close" or "on_start" (FR-INV-20).
	StockIssueMode string
}

// Garages reads a garage's settings; implemented in cmd/server over Tenant.
type Garages interface {
	Get(ctx context.Context, garageID string) (GarageSettings, error)
}

// Directory names suppliers and users for documents, so Inventory imports
// neither the Supplier nor the Identity module; implemented in cmd/server.
type Directory interface {
	// SupplierNames returns the name per supplier id of the garage, active or
	// not; unknown ids are absent.
	SupplierNames(ctx context.Context, garageID string, ids []string) (map[string]string, error)
	// UserName returns a user's full name, "" when unknown.
	UserName(ctx context.Context, userID string) (string, error)
}

// Sequencer hands out document sequence numbers (FR-TEN-05); implemented by
// internal/platform/numbering.Sequencer.
type Sequencer interface {
	Next(ctx context.Context, garageID, docType, period string) (int, error)
}

// AuditLogger records audited changes in the caller's transaction
// (Security Architecture Section 7, NFR-SE-07).
type AuditLogger interface {
	Record(ctx context.Context, e audit.Entry) error
}

// EventPublisher writes domain events to the outbox in the caller's
// transaction.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, userID string, payload any) error
}
