package usecase

import (
	"context"
	"encoding/json"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

// Cancel cancels a repair order whose work has not started (Release 7.3): it
// is open or waiting on parts, and no line has moved. Inventory releases the
// reservation of its draft stock issue from repair_order.cancelled. The quote
// the order came from stays converted. An order whose stock issue is already
// posted cannot be cancelled: its parts have left stock.
func (uc *RepairOrderUseCase) Cancel(ctx context.Context, garageID, repairOrderID, actorID string) (*domain.RepairOrder, error) {
	const op = "cancel repair order"

	ro, err := uc.load(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if (ro.Status != domain.StatusOpen && ro.Status != domain.StatusWaitingOnParts) || ro.WorkStarted() {
		return nil, toAppError(op, domain.ErrNotCancellable)
	}

	if err := uc.checkPartsMayChange(ctx, ro); err != nil {
		return nil, toAppError(op, err)
	}

	ro.Status, ro.WaitingOnParts = domain.StatusCancelled, false
	ro.UpdatedBy = actorRef(actorID)

	if err := uc.RepairOrders.Save(ctx, ro); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.publish(ctx, EventRepairOrderCancelled, ro); err != nil {
		return nil, toAppError(op, err)
	}

	return ro, nil
}

// shortagePayload is the stock_issue.shortage_changed event of Inventory
// (Release 7.3): the draft stock issue of the order was found coverable or not
// by the stock on hand. The event only says which order to look at.
type shortagePayload struct {
	RepairOrderID string `json:"repair_order_id"`
}

// HandleStockShortage is the stock_issue.shortage_changed consumer: it marks
// the order waiting on parts while Inventory cannot cover its draft stock
// issue, and clears the mark when it can (FR-INV-15). The answer is read from
// Inventory when the event is handled rather than taken from the event, so a
// late, repeated or reordered event converges on the current state. A closed,
// cancelled or unknown order is left alone.
func (uc *RepairOrderUseCase) HandleStockShortage(ctx context.Context, event outbox.Event) error {
	var payload shortagePayload
	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.RepairOrderID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry, so it is dropped
	}

	ro, err := uc.load(ctx, event.GarageID, payload.RepairOrderID)
	if err != nil {
		if errorsIsNotFound(err) {
			return nil
		}

		return err
	}

	if ro.Status == domain.StatusClosed || ro.Status == domain.StatusCancelled {
		return nil
	}

	short, err := uc.StockIssues.Short(ctx, event.GarageID, payload.RepairOrderID)
	if err != nil {
		return err
	}

	if ro.WaitingOnParts == short {
		return nil
	}

	ro.WaitingOnParts = short
	ro.DeriveStatus()

	return uc.RepairOrders.Save(ctx, ro)
}
