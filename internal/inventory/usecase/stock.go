package usecase

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// Events published by the Inventory module (Event Design Section 4).
const (
	EventStockIssued = "stock.issued"

	keyGarageID      = "garage_id"
	keyRepairOrderID = "repair_order_id"
)

// maxLowStockScan bounds how many parts a low-stock listing examines.
const maxLowStockScan = 10_000

// Dependencies are the collaborators of an InventoryUseCase.
type Dependencies struct {
	Repo      Repository
	Parts     PartCatalogue
	Events    EventPublisher
	Garages   Garages
	Directory Directory
	Sequences Sequencer
	Audit     AuditLogger
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

// InventoryUseCase is the Inventory module's application service.
type InventoryUseCase struct {
	repo      Repository
	parts     PartCatalogue
	events    EventPublisher
	garages   Garages
	directory Directory
	sequences Sequencer
	audit     AuditLogger
	now       func() time.Time
}

// NewInventoryUseCase wires the use case to its repository, to the
// Catalogue, Tenant and Supplier views, to the outbox and to the audit log.
func NewInventoryUseCase(d Dependencies) *InventoryUseCase {
	now := d.Now
	if now == nil {
		now = time.Now
	}

	return &InventoryUseCase{
		repo: d.Repo, parts: d.Parts, events: d.Events, garages: d.Garages, directory: d.Directory,
		sequences: d.Sequences, audit: d.Audit, now: now,
	}
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

func stockLevel(p PartInfo, bal Balance) *domain.StockLevel {
	onHand, reserved := orDefault(bal.OnHand, "0"), orDefault(bal.Reserved, "0")

	level := &domain.StockLevel{
		PartID: p.ID, SKU: p.SKU, Name: p.Name, BaseUnitName: p.BaseUnitName, BaseUnitAbbreviation: p.BaseUnitAbbreviation,
		CategoryName: p.CategoryName, Brand: p.Brand, IsActive: p.IsActive, ThumbnailURL: p.ThumbnailURL,
		QuantityOnHand: onHand, ReorderThreshold: p.ReorderThreshold,
		QuantityReserved: reserved, QuantityAvailable: domain.Subtract(onHand, reserved),
		LowStock: domain.IsLow(onHand, p.ReorderThreshold),
	}

	if bal.AverageCost != "" {
		level.AverageCost = &bal.AverageCost
		value := domain.StockValue(onHand, bal.AverageCost)
		level.StockValue = &value
	}

	return level
}

// orDefault returns s, or fallback when s is empty.
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}

	return s
}

func partIDs(parts []PartInfo) []string {
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.ID)
	}

	return ids
}

// ListTransactionsRequest is the input for InventoryUseCase.ListTransactions.
type ListTransactionsRequest struct {
	PageRequest

	GarageID string
	PartID   string
	Type     domain.TransactionType
	RefType  domain.RefType
}

// ListTransactions returns one page of the garage's stock movements
// (FR-INV-05), newest first, with the part and unit descriptions of the
// Catalogue.
func (uc *InventoryUseCase) ListTransactions(ctx context.Context, req ListTransactionsRequest) (*Page[*domain.StockTransaction], error) {
	page := req.normalize()

	items, total, err := uc.repo.ListTransactions(ctx, req.GarageID, TransactionFilter{
		PartID: req.PartID, Type: req.Type, RefType: req.RefType, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list stock transactions", err)
	}

	if err := uc.describe(ctx, req.GarageID, items); err != nil {
		return nil, toAppError("list stock transactions", err)
	}

	if err := uc.nameActors(ctx, items); err != nil {
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

// nameActors fills the name of the user behind each movement.
func (uc *InventoryUseCase) nameActors(ctx context.Context, txs []*domain.StockTransaction) error {
	ids := make([]string, 0, len(txs))
	for _, t := range txs {
		ids = append(ids, derefAll(t.CreatedBy)...)
	}

	names, err := uc.userNames(ctx, ids)
	if err != nil {
		return err
	}

	for _, t := range txs {
		t.CreatedByName = nameOf(names, t.CreatedBy)
	}

	return nil
}
