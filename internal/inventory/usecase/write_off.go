package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// Audit actions and document type of the write-off.
const (
	ActionWriteOffCreated   = "write_off.created"
	ActionWriteOffCancelled = "write_off.cancelled"
	entityWriteOff          = "write_off"

	docTypeWriteOff = "write_off"
)

// CreateWriteOffRequest is the input for InventoryUseCase.CreateWriteOff.
type CreateWriteOffRequest struct {
	GarageID string
	ActorID  string
	Reason   domain.WriteOffReason
	Notes    string
	Lines    []AdjustmentLineInput
}

// CreateWriteOff posts a write-off (FR-INV-09): stock falls by each line at the
// average cost of the moment. It is refused with domain.ErrInsufficientStock
// when a part is not on hand in that quantity.
func (uc *InventoryUseCase) CreateWriteOff(ctx context.Context, req CreateWriteOffRequest) (*domain.WriteOff, error) {
	const op = "create write-off"

	if !req.Reason.IsValid() {
		return nil, toAppError(op, domain.ErrInvalidWriteOffReason)
	}

	garage, err := uc.garages.Get(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	lines, err := uc.buildAdjustmentLines(ctx, req.GarageID, req.Lines)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if _, err := uc.takeStock(ctx, req.GarageID, lines); err != nil {
		return nil, toAppError(op, err)
	}

	number, err := uc.nextNumber(ctx, req.GarageID, docTypeWriteOff, garage.WriteOffNumberTemplate, garage.Timezone)
	if err != nil {
		return nil, toAppError(op, err)
	}

	w := &domain.WriteOff{
		ID: uuid.NewString(), GarageID: req.GarageID, Number: number, Status: domain.AdjustmentCompleted, Reason: req.Reason,
		Notes: req.Notes, Lines: lines, CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	m := movement{garageID: req.GarageID, refType: domain.RefWriteOff, refID: w.ID, reason: string(w.Reason), actorID: req.ActorID}
	if err := uc.takeOutLines(ctx, m, w.Lines); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.repo.CreateWriteOff(ctx, w); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.finishWriteOffs(ctx, req.GarageID, w); err != nil {
		return nil, toAppError(op, err)
	}

	err = uc.auditAdjustment(ctx, req.ActorID, req.GarageID, ActionWriteOffCreated, entityWriteOff, w.ID, nil,
		map[string]any{keyNumber: w.Number, "reason": w.Reason, keyStatus: w.Status, keyLines: len(w.Lines), "total_value": w.TotalValue})
	if err != nil {
		return nil, err
	}

	return w, nil
}

// GetWriteOff returns one write-off with its lines.
func (uc *InventoryUseCase) GetWriteOff(ctx context.Context, garageID, writeOffID string) (*domain.WriteOff, error) {
	w, err := uc.repo.FindWriteOff(ctx, garageID, writeOffID, false)
	if err != nil {
		return nil, toAppError("get write-off", err)
	}

	if err := uc.finishWriteOffs(ctx, garageID, w); err != nil {
		return nil, toAppError("get write-off", err)
	}

	return w, nil
}

// ListWriteOffsRequest is the input for InventoryUseCase.ListWriteOffs.
type ListWriteOffsRequest struct {
	PageRequest

	GarageID string
	Query    string
	Reason   domain.WriteOffReason
	Status   domain.AdjustmentStatus
}

// ListWriteOffs returns one page of the garage's write-offs, newest first,
// without lines.
func (uc *InventoryUseCase) ListWriteOffs(ctx context.Context, req ListWriteOffsRequest) (*Page[*domain.WriteOff], error) {
	page := req.normalize()

	items, total, err := uc.repo.ListWriteOffs(ctx, req.GarageID, WriteOffFilter{
		Query: req.Query, Reason: req.Reason, Status: req.Status, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list write-offs", err)
	}

	users, err := uc.userNames(ctx, creatorIDs(items, func(w *domain.WriteOff) *string { return w.CreatedBy }))
	if err != nil {
		return nil, toAppError("list write-offs", err)
	}

	for _, w := range items {
		w.CreatedByName = nameOf(users, w.CreatedBy)
	}

	return &Page[*domain.WriteOff]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// CancelWriteOff puts the parts of a write-off back in stock, at the cost they
// left with. A cancelled write-off cannot be cancelled again.
func (uc *InventoryUseCase) CancelWriteOff(ctx context.Context, garageID, writeOffID, actorID string) (*domain.WriteOff, error) {
	const op = "cancel write-off"

	w, err := uc.repo.FindWriteOff(ctx, garageID, writeOffID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if w.Status == domain.AdjustmentCancelled {
		return nil, toAppError(op, domain.ErrWriteOffAlreadyCancelled)
	}

	m := movement{garageID: garageID, refType: domain.RefWriteOff, refID: w.ID, reason: string(w.Reason), actorID: actorID}
	if err := uc.putBackLines(ctx, m, w.Lines); err != nil {
		return nil, toAppError(op, err)
	}

	now := uc.now()
	w.Status, w.CancelledAt, w.UpdatedBy = domain.AdjustmentCancelled, &now, actorRef(actorID)

	if err := uc.repo.SaveWriteOff(ctx, w); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.finishWriteOffs(ctx, garageID, w); err != nil {
		return nil, toAppError(op, err)
	}

	err = uc.auditAdjustment(ctx, actorID, garageID, ActionWriteOffCancelled, entityWriteOff, w.ID,
		map[string]any{keyStatus: domain.AdjustmentCompleted}, map[string]any{keyNumber: w.Number, keyStatus: w.Status})
	if err != nil {
		return nil, err
	}

	return w, nil
}

// finishWriteOffs fills the derived descriptions and the total of write-offs
// that carry their lines.
func (uc *InventoryUseCase) finishWriteOffs(ctx context.Context, garageID string, w *domain.WriteOff) error {
	if err := uc.describeAdjustmentLines(ctx, garageID, w.Lines); err != nil {
		return err
	}

	users, err := uc.userNames(ctx, derefAll(w.CreatedBy))
	if err != nil {
		return err
	}

	w.CreatedByName, w.TotalValue = nameOf(users, w.CreatedBy), w.Total()

	return nil
}

// creatorIDs returns the non-nil creator ids of items.
func creatorIDs[T any](items []T, creator func(T) *string) []string {
	ids := make([]string, 0, len(items))

	for _, it := range items {
		ids = append(ids, derefAll(creator(it))...)
	}

	return ids
}
