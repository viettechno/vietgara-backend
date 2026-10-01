package usecase

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// InventoryUseCase implements every action of the Inventory module.
type InventoryUseCase struct {
	repo   Repository
	events EventPublisher
}

// NewInventoryUseCase wires the use case to its dependencies.
func NewInventoryUseCase(repo Repository, events EventPublisher) *InventoryUseCase {
	return &InventoryUseCase{repo: repo, events: events}
}

// ItemFields are the settable item fields; nil fields are left unchanged
// (on create: defaulted).
type ItemFields struct {
	SKU              *string
	Name             *string
	Unit             *string
	ReorderThreshold *int
}

// CreateItemRequest is the input for InventoryUseCase.CreateItem.
type CreateItemRequest struct {
	GarageID string
	ActorID  string
	Fields   ItemFields
}

// CreateItem adds a part code to the garage's catalog.
func (uc *InventoryUseCase) CreateItem(ctx context.Context, req CreateItemRequest) (*domain.Item, error) {
	item := &domain.Item{
		ID: uuid.NewString(), GarageID: req.GarageID, QuantityOnHand: "0",
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	if req.Fields.SKU == nil || req.Fields.Name == nil {
		return nil, toAppError("create item", requiredFieldError(req.Fields))
	}

	if err := applyItemFields(item, req.Fields); err != nil {
		return nil, toAppError("create item", err)
	}

	if err := uc.repo.CreateItem(ctx, item); err != nil {
		return nil, toAppError("create item", err)
	}

	return item, nil
}

func requiredFieldError(f ItemFields) error {
	if f.SKU == nil {
		return domain.ErrSKURequired
	}

	return domain.ErrNameRequired
}

// GetItem returns one item of the garage, with its current quantity on hand.
func (uc *InventoryUseCase) GetItem(ctx context.Context, garageID, itemID string) (*domain.Item, error) {
	item, err := uc.repo.FindItemByID(ctx, garageID, itemID)
	if err != nil {
		return nil, toAppError("get item", err)
	}

	return item, nil
}

// UpdateItemRequest is the input for InventoryUseCase.UpdateItem.
type UpdateItemRequest struct {
	GarageID string
	ItemID   string
	ActorID  string
	Fields   ItemFields
}

// UpdateItem applies the partial updates in req to an item.
func (uc *InventoryUseCase) UpdateItem(ctx context.Context, req UpdateItemRequest) (*domain.Item, error) {
	item, err := uc.repo.FindItemByID(ctx, req.GarageID, req.ItemID)
	if err != nil {
		return nil, toAppError("update item", err)
	}

	if err := applyItemFields(item, req.Fields); err != nil {
		return nil, toAppError("update item", err)
	}

	item.UpdatedBy = actorRef(req.ActorID)

	if err := uc.repo.UpdateItem(ctx, item); err != nil {
		return nil, toAppError("update item", err)
	}

	return item, nil
}

// ListItemsRequest is the input for InventoryUseCase.ListItems.
type ListItemsRequest struct {
	PageRequest

	GarageID string
	Query    string
}

// ListItems returns one page of the garage's items, each with its current
// quantity on hand (FR-INV-03).
func (uc *InventoryUseCase) ListItems(ctx context.Context, req ListItemsRequest) (*Page[*domain.Item], error) {
	page := req.normalize()

	items, total, err := uc.repo.ListItems(ctx, req.GarageID, ListFilter{Query: strings.TrimSpace(req.Query), Limit: page.PageSize, Offset: page.offset()})
	if err != nil {
		return nil, toAppError("list items", err)
	}

	return &Page[*domain.Item]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// DeleteItem soft-deletes an item; past stock transactions keep
// referencing it.
func (uc *InventoryUseCase) DeleteItem(ctx context.Context, garageID, itemID, actorID string) error {
	if err := uc.repo.SoftDeleteItem(ctx, garageID, itemID, actorID); err != nil {
		return toAppError("delete item", err)
	}

	return nil
}

func applyItemFields(item *domain.Item, f ItemFields) error {
	if f.SKU != nil {
		sku := strings.TrimSpace(*f.SKU)
		if sku == "" {
			return domain.ErrSKURequired
		}

		if err := domain.CheckLength(sku, domain.MaxSKULen); err != nil {
			return err
		}

		item.SKU = sku
	}

	if f.Name != nil {
		name := strings.TrimSpace(*f.Name)
		if name == "" {
			return domain.ErrNameRequired
		}

		if err := domain.CheckLength(name, domain.MaxNameLen); err != nil {
			return err
		}

		item.Name = name
	}

	if f.Unit != nil {
		unit := strings.TrimSpace(*f.Unit)
		if err := domain.CheckLength(unit, domain.MaxUnitLen); err != nil {
			return err
		}

		item.Unit = unit
	}

	if f.ReorderThreshold != nil {
		if *f.ReorderThreshold < 0 {
			return domain.ErrInvalidReorderThreshold
		}

		item.ReorderThreshold = *f.ReorderThreshold
	}

	return nil
}
