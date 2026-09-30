package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

// partLinePayload is one part line of the repair_order.closed event
// (Event Design Section 3): enough for Inventory to deduct stock
// (FR-INV-02) without querying this module's tables directly.
type partLinePayload struct {
	ItemRefID string `json:"item_ref_id"`
	Quantity  string `json:"quantity"`
}

// Close closes the repair order (FR-REP-06): fails FAILED_PRECONDITION
// unless every line is completed (ReadyToClose). Publishes
// repair_order.closed in the same transaction; the Inventory module
// deducts stock for the part lines asynchronously from that event (Event
// Design), not synchronously here -- closing a repair order must not fail
// because of an inventory-side problem.
func (uc *RepairOrderUseCase) Close(ctx context.Context, garageID, repairOrderID, actorID string) (*domain.RepairOrder, error) {
	ro, err := uc.RepairOrders.FindByID(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError("close repair order", err)
	}

	if ro.Status == domain.StatusClosed {
		return nil, toAppError("close repair order", domain.ErrAlreadyClosed)
	}

	if !ro.ReadyToClose() {
		return nil, toAppError("close repair order", domain.ErrNotReadyToClose)
	}

	now := uc.Now()
	ro.Status = domain.StatusClosed
	ro.ClosedAt = &now
	ro.UpdatedBy = actorRef(actorID)

	if err := uc.RepairOrders.Save(ctx, ro); err != nil {
		return nil, toAppError("close repair order", err)
	}

	partLines := make([]partLinePayload, 0, len(ro.PartLines()))
	for _, line := range ro.PartLines() {
		partLines = append(partLines, partLinePayload{ItemRefID: *line.ItemRefID, Quantity: line.Quantity})
	}

	err = uc.Events.Publish(ctx, EventRepairOrderClosed, garageID, "", map[string]any{
		"repair_order_id": ro.ID, keyGarageID: garageID, "part_lines": partLines,
	})
	if err != nil {
		return nil, toAppError("close repair order", err)
	}

	return ro, nil
}
