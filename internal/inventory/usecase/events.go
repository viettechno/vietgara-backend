package usecase

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// partLinePayload is one part line of the repair_order.closed event
// (Event Design Section 3), matching the Repair Order module's own
// publishing type.
type partLinePayload struct {
	ItemRefID string `json:"item_ref_id"`
	Quantity  string `json:"quantity"`
}

type repairOrderClosedPayload struct {
	RepairOrderID string            `json:"repair_order_id"`
	PartLines     []partLinePayload `json:"part_lines"`
}

// HandleRepairOrderClosed is the repair_order.closed consumer (FR-INV-02):
// deducts stock for each part line the closed repair order used. A line
// whose item no longer resolves (deleted, or never carried in stock) is
// logged and skipped rather than failing the whole event -- one bad line
// must not block every other line's deduction, or the event's retries.
func (uc *InventoryUseCase) HandleRepairOrderClosed(ctx context.Context, event outbox.Event) error {
	var payload repairOrderClosedPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.RepairOrderID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	for _, line := range payload.PartLines {
		if line.ItemRefID == "" {
			continue
		}

		if err := uc.IssueStock(ctx, event.GarageID, line.ItemRefID, line.Quantity, payload.RepairOrderID); err != nil {
			slog.Warn("inventory: issuing stock for a closed repair order failed, skipping the line",
				"repair_order_id", payload.RepairOrderID, "item_id", line.ItemRefID, "error", err)
		}
	}

	return nil
}
