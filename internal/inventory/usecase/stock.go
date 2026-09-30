package usecase

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// Events published by the Inventory module (Event Design Section 4).
const (
	EventStockIssued = "stock.issued"

	keyGarageID = "garage_id"
)

// CreateStockReceiptRequest is the input for InventoryUseCase.CreateStockReceipt.
type CreateStockReceiptRequest struct {
	GarageID string
	ActorID  string
	ItemID   string
	Quantity string
	// UnitCost is optional (0 means "not recorded").
	UnitCost int64
}

// StockReceiptResult is the outcome of InventoryUseCase.CreateStockReceipt.
type StockReceiptResult struct {
	Transaction *domain.StockTransaction
	Item        *domain.Item
}

// CreateStockReceipt records a goods-receipt note from a supplier
// (FR-INV-01), increasing the item's stock on hand.
func (uc *InventoryUseCase) CreateStockReceipt(ctx context.Context, req CreateStockReceiptRequest) (StockReceiptResult, error) {
	item, err := uc.repo.FindItemByID(ctx, req.GarageID, req.ItemID)
	if err != nil {
		return StockReceiptResult{}, toAppError("create stock receipt", err)
	}

	quantity, err := domain.ParseQuantity(req.Quantity)
	if err != nil {
		return StockReceiptResult{}, toAppError("create stock receipt", err)
	}

	var unitCost *int64
	if req.UnitCost > 0 {
		unitCost = &req.UnitCost
	}

	tx := &domain.StockTransaction{
		ID: uuid.NewString(), GarageID: req.GarageID, ItemID: item.ID, Type: domain.TransactionIn, Quantity: quantity,
		UnitCost: unitCost, RefType: domain.RefPurchase, CreatedBy: actorRef(req.ActorID),
	}

	if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
		return StockReceiptResult{}, toAppError("create stock receipt", err)
	}

	updated, err := uc.repo.FindItemByID(ctx, req.GarageID, req.ItemID)
	if err != nil {
		return StockReceiptResult{}, toAppError("create stock receipt", err)
	}

	tx.ItemSKU, tx.ItemName = updated.SKU, updated.Name

	return StockReceiptResult{Transaction: tx, Item: updated}, nil
}

// ListTransactionsRequest is the input for InventoryUseCase.ListTransactions.
type ListTransactionsRequest struct {
	PageRequest

	GarageID string
	ItemID   string
	Type     domain.TransactionType
}

// ListTransactions returns one page of the garage's stock movements
// (FR-INV-05), newest first.
func (uc *InventoryUseCase) ListTransactions(ctx context.Context, req ListTransactionsRequest) (*Page[*domain.StockTransaction], error) {
	page := req.normalize()

	items, total, err := uc.repo.ListTransactions(ctx, req.GarageID, TransactionFilter{
		ItemID: req.ItemID, Type: req.Type, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list stock transactions", err)
	}

	return &Page[*domain.StockTransaction]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// IssueStock deducts quantity from an item's stock on hand for a closed
// repair order's part line (FR-INV-02), triggered by the
// repair_order.closed event consumer. An unknown item or an already
// non-existent line is not an error the caller should retry over -- see
// the consumer, which logs and moves on rather than blocking the whole
// event on one bad line. A repeat delivery for the same (item, repair
// order) is a no-op (domain.ErrAlreadyIssued), not a double deduction.
func (uc *InventoryUseCase) IssueStock(ctx context.Context, garageID, itemID, quantity, repairOrderID string) error {
	item, err := uc.repo.FindItemByID(ctx, garageID, itemID)
	if err != nil {
		return toAppError("issue stock", err)
	}

	qty, err := domain.ParseQuantity(quantity)
	if err != nil {
		return toAppError("issue stock", err)
	}

	refID := repairOrderID
	tx := &domain.StockTransaction{
		ID: uuid.NewString(), GarageID: garageID, ItemID: item.ID, Type: domain.TransactionOut, Quantity: qty,
		RefType: domain.RefRepairOrder, RefID: &refID,
	}

	if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
		if errors.Is(err, domain.ErrAlreadyIssued) {
			return nil
		}

		return toAppError("issue stock", err)
	}

	return uc.events.Publish(ctx, EventStockIssued, garageID, "", map[string]any{
		"item_id": item.ID, keyGarageID: garageID, "repair_order_id": repairOrderID, "quantity": qty,
	})
}
