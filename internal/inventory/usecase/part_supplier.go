package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// GetPartSuppliers returns the usual suppliers of a part (FR-INV-19) with
// their code, last cost and default flag.
func (uc *InventoryUseCase) GetPartSuppliers(ctx context.Context, garageID, partID string) ([]domain.PartSupplier, error) {
	const op = "get part suppliers"

	part, err := uc.part(ctx, garageID, partID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	list, err := uc.repo.PartSuppliers(ctx, garageID, part.ID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.nameSuppliers(ctx, garageID, list); err != nil {
		return nil, toAppError(op, err)
	}

	return list, nil
}

// PutPartSuppliers replaces the part's list of usual suppliers as a whole. A
// supplier may be listed once and at most one is the default. The last cost
// of a supplier that stays on the list is kept; it is set by completing
// receipts, not typed.
func (uc *InventoryUseCase) PutPartSuppliers(ctx context.Context, garageID, partID string, list []domain.PartSupplier) ([]domain.PartSupplier, error) {
	const op = "put part suppliers"

	part, err := uc.part(ctx, garageID, partID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := domain.ValidatePartSuppliers(list); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.nameSuppliers(ctx, garageID, list); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.repo.ReplacePartSuppliers(ctx, garageID, part.ID, list); err != nil {
		return nil, toAppError(op, err)
	}

	stored, err := uc.repo.PartSuppliers(ctx, garageID, part.ID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.nameSuppliers(ctx, garageID, stored); err != nil {
		return nil, toAppError(op, err)
	}

	return stored, nil
}

// nameSuppliers fills the derived supplier names, failing with
// domain.ErrSupplierNotFound for a supplier that is not the garage's.
func (uc *InventoryUseCase) nameSuppliers(ctx context.Context, garageID string, list []domain.PartSupplier) error {
	ids := make([]string, 0, len(list))
	for _, s := range list {
		ids = append(ids, s.SupplierID)
	}

	names, err := uc.supplierNames(ctx, garageID, ids)
	if err != nil {
		return err
	}

	for i := range list {
		name, ok := names[list[i].SupplierID]
		if !ok {
			return domain.ErrSupplierNotFound
		}

		list[i].SupplierName = name
	}

	return nil
}
