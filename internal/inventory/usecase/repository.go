// Package usecase contains the application use cases of the Inventory
// module (FRD 3.7): the stock on hand of the Catalogue's parts (FR-INV-03),
// stock receipts from suppliers (FR-INV-01), automatic stock deduction when
// a repair order with part lines closes (FR-INV-02), and the stock movement
// report (FR-INV-05). Every method expects to run inside a garage-scoped
// transaction (see middleware.Garage) so Row-Level Security applies. What a
// part is, and how a quantity typed in a packaging unit becomes a base-unit
// quantity, is the Catalogue's business and reached through PartCatalogue.
// See: Coding_Standards.docx Section 2.3, HLD Section 4.
package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// TransactionFilter selects one page of a garage's stock transactions
// (FR-INV-05).
type TransactionFilter struct {
	PartID string
	Type   domain.TransactionType
	Limit  int
	Offset int
}

// Repository defines the persistence contract for stock balances and stock
// movements. Every method is scoped to the given garage.
type Repository interface {
	// Balances returns the quantity on hand per part id (decimal strings);
	// a part without movements is absent. With ids empty it returns every
	// part of the garage that has a balance.
	Balances(ctx context.Context, garageID string, ids []string) (map[string]string, error)

	// RecordTransaction inserts tx and atomically applies its effect to
	// stock_balance (+quantity for TransactionIn, -quantity for
	// TransactionOut) in the same statement group. A repair-order issue
	// already recorded for (garage, part, repair order) returns
	// domain.ErrAlreadyIssued instead of a duplicate row.
	RecordTransaction(ctx context.Context, tx *domain.StockTransaction) error
	// ListTransactions returns one page of transactions (FR-INV-05),
	// newest first, without the derived part and unit descriptions.
	ListTransactions(ctx context.Context, garageID string, filter TransactionFilter) ([]*domain.StockTransaction, int, error)
}

// PartInfo is the Catalogue's description of a part, as Inventory needs it.
type PartInfo struct {
	ID                   string
	SKU                  string
	Name                 string
	BaseUnitName         string
	BaseUnitAbbreviation string
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

// EventPublisher writes domain events to the outbox in the caller's
// transaction.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, userID string, payload any) error
}
