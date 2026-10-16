package usecase

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// Events published by the Inventory module (Event Design Section 4).
const (
	EventStockIssued = "stock.issued"

	keyGarageID = "garage_id"
)

// maxLowStockScan bounds how many parts a low-stock listing examines.
const maxLowStockScan = 10_000

// InventoryUseCase is the Inventory module's application service.
type InventoryUseCase struct {
	repo   Repository
	parts  PartCatalogue
	events EventPublisher
}

// NewInventoryUseCase wires the use case to its repository, to the
// Catalogue and to the outbox.
func NewInventoryUseCase(repo Repository, parts PartCatalogue, events EventPublisher) *InventoryUseCase {
	return &InventoryUseCase{repo: repo, parts: parts, events: events}
}

// ListStockLevelsRequest is the input for InventoryUseCase.ListStockLevels.
type ListStockLevelsRequest struct {
	PageRequest

	GarageID string
	// Query matches SKU, name or brand.
	Query string
	// CategoryID also matches the sub-categories.
	CategoryID      string
	Brand           string
	LowStock        bool
	IncludeInactive bool
}

// ListStockLevels returns one page of the garage's parts with their stock on
// hand (FR-INV-03), ordered by name. The parts come from the Catalogue and
// the quantities from Inventory's balances; with LowStock only parts at or
// below their reorder threshold are listed.
func (uc *InventoryUseCase) ListStockLevels(ctx context.Context, req ListStockLevelsRequest) (*Page[*domain.StockLevel], error) {
	page := req.normalize()
	query := PartQuery{Query: req.Query, CategoryID: req.CategoryID, Brand: req.Brand, IncludeInactive: req.IncludeInactive}

	if req.LowStock {
		return uc.listLowStock(ctx, req.GarageID, query, page)
	}

	query.Limit, query.Offset = page.PageSize, page.offset()

	parts, total, err := uc.parts.SearchParts(ctx, req.GarageID, query)
	if err != nil {
		return nil, toAppError("list stock levels", err)
	}

	balances, err := uc.repo.Balances(ctx, req.GarageID, partIDs(parts))
	if err != nil {
		return nil, toAppError("list stock levels", err)
	}

	levels := make([]*domain.StockLevel, 0, len(parts))
	for _, p := range parts {
		levels = append(levels, stockLevel(p, balances[p.ID]))
	}

	return &Page[*domain.StockLevel]{Items: levels, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// listLowStock pages through every matching part, because the threshold
// (Catalogue) and the quantity (Inventory) are only comparable together.
func (uc *InventoryUseCase) listLowStock(ctx context.Context, garageID string, query PartQuery, page PageRequest) (*Page[*domain.StockLevel], error) {
	balances, err := uc.repo.Balances(ctx, garageID, nil)
	if err != nil {
		return nil, toAppError("list stock levels", err)
	}

	var low []*domain.StockLevel

	query.Limit = MaxPageSize

	for query.Offset = 0; query.Offset < maxLowStockScan; query.Offset += MaxPageSize {
		parts, total, err := uc.parts.SearchParts(ctx, garageID, query)
		if err != nil {
			return nil, toAppError("list stock levels", err)
		}

		for _, p := range parts {
			if level := stockLevel(p, balances[p.ID]); level.LowStock {
				low = append(low, level)
			}
		}

		if query.Offset+MaxPageSize >= total {
			break
		}
	}

	from := min(page.offset(), len(low))
	to := min(from+page.PageSize, len(low))

	return &Page[*domain.StockLevel]{Items: low[from:to], Total: len(low), Page: page.Page, PageSize: page.PageSize}, nil
}

// GetStockLevel returns one part with its stock on hand.
func (uc *InventoryUseCase) GetStockLevel(ctx context.Context, garageID, partID string) (*domain.StockLevel, error) {
	part, err := uc.part(ctx, garageID, partID)
	if err != nil {
		return nil, toAppError("get stock level", err)
	}

	return uc.level(ctx, garageID, part)
}

// part returns the Catalogue's part, or domain.ErrPartNotFound.
func (uc *InventoryUseCase) part(ctx context.Context, garageID, partID string) (PartInfo, error) {
	if uuid.Validate(partID) != nil {
		return PartInfo{}, domain.ErrPartNotFound
	}

	parts, err := uc.parts.Parts(ctx, garageID, []string{partID})
	if err != nil {
		return PartInfo{}, err
	}

	if len(parts) == 0 {
		return PartInfo{}, domain.ErrPartNotFound
	}

	return parts[0], nil
}

func (uc *InventoryUseCase) level(ctx context.Context, garageID string, part PartInfo) (*domain.StockLevel, error) {
	balances, err := uc.repo.Balances(ctx, garageID, []string{part.ID})
	if err != nil {
		return nil, toAppError("get stock level", err)
	}

	return stockLevel(part, balances[part.ID]), nil
}

func stockLevel(p PartInfo, quantity string) *domain.StockLevel {
	if quantity == "" {
		quantity = "0"
	}

	return &domain.StockLevel{
		PartID: p.ID, SKU: p.SKU, Name: p.Name, BaseUnitName: p.BaseUnitName, BaseUnitAbbreviation: p.BaseUnitAbbreviation,
		CategoryName: p.CategoryName, Brand: p.Brand, IsActive: p.IsActive, ThumbnailURL: p.ThumbnailURL,
		QuantityOnHand: quantity, ReorderThreshold: p.ReorderThreshold,
		LowStock: domain.IsLow(quantity, p.ReorderThreshold),
	}
}

func partIDs(parts []PartInfo) []string {
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.ID)
	}

	return ids
}

// CreateStockReceiptRequest is the input for InventoryUseCase.CreateStockReceipt.
type CreateStockReceiptRequest struct {
	GarageID string
	ActorID  string
	PartID   string
	// Quantity is typed in UnitID (empty: the part's base unit).
	Quantity string
	UnitID   string
	// UnitCost is the cost of one entered unit; 0 means "not recorded".
	UnitCost int64
}

// StockReceiptResult is the outcome of InventoryUseCase.CreateStockReceipt.
type StockReceiptResult struct {
	Transaction *domain.StockTransaction
	StockLevel  *domain.StockLevel
}

// CreateStockReceipt records a goods-receipt note from a supplier
// (FR-INV-01), increasing the part's stock on hand. The quantity is
// converted to the base unit by the Catalogue and the cost, typed per entered
// unit, is stored per base unit.
func (uc *InventoryUseCase) CreateStockReceipt(ctx context.Context, req CreateStockReceiptRequest) (StockReceiptResult, error) {
	part, err := uc.part(ctx, req.GarageID, req.PartID)
	if err != nil {
		return StockReceiptResult{}, toAppError("create stock receipt", err)
	}

	if !part.IsActive {
		return StockReceiptResult{}, toAppError("create stock receipt", domain.ErrPartInactive)
	}

	if req.UnitCost < 0 {
		return StockReceiptResult{}, toAppError("create stock receipt", domain.ErrInvalidCost)
	}

	resolved, err := uc.parts.ResolveQuantity(ctx, req.GarageID, part.ID, req.UnitID, req.Quantity)
	if err != nil {
		return StockReceiptResult{}, toAppError("create stock receipt", err)
	}

	tx := &domain.StockTransaction{
		ID: uuid.NewString(), GarageID: req.GarageID, PartID: part.ID, Type: domain.TransactionIn, Quantity: resolved.Base,
		UnitID: resolved.UnitID, EnteredQuantity: resolved.Entered, RefType: domain.RefPurchase, CreatedBy: actorRef(req.ActorID),
	}

	if req.UnitCost > 0 {
		entered := resolved.Base
		if resolved.Entered != nil {
			entered = *resolved.Entered
		}

		cost, err := domain.CostPerBaseUnit(req.UnitCost, entered, resolved.Base)
		if err != nil {
			return StockReceiptResult{}, toAppError("create stock receipt", err)
		}

		tx.UnitCost = &cost
	}

	if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
		return StockReceiptResult{}, toAppError("create stock receipt", err)
	}

	if err := uc.describe(ctx, req.GarageID, []*domain.StockTransaction{tx}, part); err != nil {
		return StockReceiptResult{}, toAppError("create stock receipt", err)
	}

	level, err := uc.level(ctx, req.GarageID, part)
	if err != nil {
		return StockReceiptResult{}, err
	}

	return StockReceiptResult{Transaction: tx, StockLevel: level}, nil
}

// ListTransactionsRequest is the input for InventoryUseCase.ListTransactions.
type ListTransactionsRequest struct {
	PageRequest

	GarageID string
	PartID   string
	Type     domain.TransactionType
}

// ListTransactions returns one page of the garage's stock movements
// (FR-INV-05), newest first, with the part and unit descriptions of the
// Catalogue.
func (uc *InventoryUseCase) ListTransactions(ctx context.Context, req ListTransactionsRequest) (*Page[*domain.StockTransaction], error) {
	page := req.normalize()

	items, total, err := uc.repo.ListTransactions(ctx, req.GarageID, TransactionFilter{
		PartID: req.PartID, Type: req.Type, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list stock transactions", err)
	}

	if err := uc.describe(ctx, req.GarageID, items); err != nil {
		return nil, toAppError("list stock transactions", err)
	}

	return &Page[*domain.StockTransaction]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// describe fills the derived part SKU/name and unit abbreviation of
// transactions from the Catalogue; known parts are not asked for again.
func (uc *InventoryUseCase) describe(ctx context.Context, garageID string, txs []*domain.StockTransaction, known ...PartInfo) error {
	byID := make(map[string]PartInfo, len(known))
	for _, p := range known {
		byID[p.ID] = p
	}

	var missing, unitIDs []string

	for _, t := range txs {
		if _, ok := byID[t.PartID]; !ok && !slices.Contains(missing, t.PartID) {
			missing = append(missing, t.PartID)
		}

		if t.UnitID != nil && !slices.Contains(unitIDs, *t.UnitID) {
			unitIDs = append(unitIDs, *t.UnitID)
		}
	}

	if len(missing) > 0 {
		parts, err := uc.parts.Parts(ctx, garageID, missing)
		if err != nil {
			return err
		}

		for _, p := range parts {
			byID[p.ID] = p
		}
	}

	abbreviations, err := uc.unitAbbreviations(ctx, garageID, unitIDs)
	if err != nil {
		return err
	}

	for _, t := range txs {
		t.PartSKU, t.PartName = byID[t.PartID].SKU, byID[t.PartID].Name
		t.BaseUnitAbbreviation = byID[t.PartID].BaseUnitAbbreviation

		if t.UnitID != nil {
			t.UnitAbbreviation = abbreviations[*t.UnitID]
		}
	}

	return nil
}

func (uc *InventoryUseCase) unitAbbreviations(ctx context.Context, garageID string, unitIDs []string) (map[string]string, error) {
	if len(unitIDs) == 0 {
		return nil, nil
	}

	return uc.parts.UnitAbbreviations(ctx, garageID, unitIDs)
}

// IssueStock deducts quantity (already in the base unit) from a part's stock
// on hand for a closed repair order's part line (FR-INV-02), triggered by the
// repair_order.closed event consumer. An unknown part is not an error the
// caller should retry over -- see the consumer, which logs and moves on
// rather than blocking the whole event on one bad line. A repeat delivery for
// the same (part, repair order) is a no-op (domain.ErrAlreadyIssued), not a
// double deduction. Stock may go negative: the repair order has already been
// closed and the parts used.
func (uc *InventoryUseCase) IssueStock(ctx context.Context, garageID, partID, quantity, repairOrderID string) error {
	part, err := uc.part(ctx, garageID, partID)
	if err != nil {
		return toAppError("issue stock", err)
	}

	qty, err := domain.ParseQuantity(strings.TrimSpace(quantity))
	if err != nil {
		return toAppError("issue stock", err)
	}

	refID := repairOrderID
	tx := &domain.StockTransaction{
		ID: uuid.NewString(), GarageID: garageID, PartID: part.ID, Type: domain.TransactionOut, Quantity: qty,
		RefType: domain.RefRepairOrder, RefID: &refID,
	}

	if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
		if errors.Is(err, domain.ErrAlreadyIssued) {
			return nil
		}

		return toAppError("issue stock", err)
	}

	return uc.events.Publish(ctx, EventStockIssued, garageID, "", map[string]any{
		"item_id": part.ID, keyGarageID: garageID, "repair_order_id": repairOrderID, "quantity": qty,
	})
}
