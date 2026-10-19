package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

// partLinePayload is one part line of the repair_order.created, .started and
// .closed events (Event Design Section 3): enough for Inventory to keep the
// order's stock issue (FR-INV-20) without querying this module's tables
// directly. Quantity is in the base unit; UnitID and EnteredQuantity keep the
// packaging unit the line was typed in.
type partLinePayload struct {
	LineID          string `json:"line_id"`
	ItemRefID       string `json:"item_ref_id"`
	Quantity        string `json:"quantity"`
	UnitID          string `json:"unit_id,omitempty"`
	EnteredQuantity string `json:"entered_quantity,omitempty"`
}

// publish writes a repair order lifecycle event with the order's part lines
// to the outbox, in the caller's transaction.
func (uc *RepairOrderUseCase) publish(ctx context.Context, eventType string, ro *domain.RepairOrder) error {
	partLines := make([]partLinePayload, 0, len(ro.PartLines()))

	for _, line := range ro.PartLines() {
		p := partLinePayload{LineID: line.ID, ItemRefID: *line.ItemRefID, Quantity: line.Quantity}
		if line.UnitID != nil && line.EnteredQuantity != nil {
			p.UnitID, p.EnteredQuantity = *line.UnitID, *line.EnteredQuantity
		}

		partLines = append(partLines, p)
	}

	return uc.Events.Publish(ctx, eventType, ro.GarageID, "", map[string]any{
		"repair_order_id": ro.ID, "repair_order_number": ro.Number, keyGarageID: ro.GarageID, "part_lines": partLines,
	})
}

// Close closes the repair order (FR-REP-06): fails FAILED_PRECONDITION
// unless every line is completed (ReadyToClose). Publishes
// repair_order.closed in the same transaction; the Inventory module
// posts the order's stock issue asynchronously from that event (Event
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

	if ro.Status == domain.StatusCancelled {
		return nil, toAppError("close repair order", domain.ErrNotEditable)
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

	if err := uc.publish(ctx, EventRepairOrderClosed, ro); err != nil {
		return nil, toAppError("close repair order", err)
	}

	return ro, nil
}
