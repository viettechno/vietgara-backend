package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// partLinePayload is one part line of the repair_order.created and
// repair_order.closed events (Event Design Section 3), matching the Repair
// Order module's own publishing type.
type partLinePayload struct {
	LineID          string `json:"line_id"`
	ItemRefID       string `json:"item_ref_id"`
	Quantity        string `json:"quantity"`
	UnitID          string `json:"unit_id"`
	EnteredQuantity string `json:"entered_quantity"`
}

type repairOrderPayload struct {
	RepairOrderID     string            `json:"repair_order_id"`
	RepairOrderNumber string            `json:"repair_order_number"`
	PartLines         []partLinePayload `json:"part_lines"`
}

func decodeRepairOrder(event outbox.Event) (repairOrderPayload, bool) {
	var payload repairOrderPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.RepairOrderID == "" {
		return payload, false
	}

	return payload, true
}

func (p repairOrderPayload) request(garageID string) CreateStockIssueRequest {
	lines := make([]IssueLineInput, 0, len(p.PartLines))

	for _, l := range p.PartLines {
		if l.ItemRefID == "" {
			continue
		}

		lines = append(lines, IssueLineInput{
			RepairOrderLineID: l.LineID, PartID: l.ItemRefID, Quantity: l.Quantity,
			UnitID: l.UnitID, EnteredQuantity: l.EnteredQuantity,
		})
	}

	return CreateStockIssueRequest{
		GarageID: garageID, RepairOrderID: p.RepairOrderID, RepairOrderNumber: p.RepairOrderNumber, Lines: lines,
	}
}

// HandleRepairOrderCreated is the repair_order.created consumer (FR-INV-20):
// creates the draft stock issue, which reserves the order's parts. A
// malformed event cannot succeed on retry and is dropped.
func (uc *InventoryUseCase) HandleRepairOrderCreated(ctx context.Context, event outbox.Event) error {
	payload, ok := decodeRepairOrder(event)
	if !ok {
		return nil
	}

	_, err := uc.CreateStockIssue(ctx, payload.request(event.GarageID))

	return err
}

// HandleRepairOrderStarted is the repair_order.started consumer: with the
// garage setting stock_issue_mode = on_start, the order's stock issue posts
// now instead of on close (FR-INV-20).
func (uc *InventoryUseCase) HandleRepairOrderStarted(ctx context.Context, event outbox.Event) error {
	payload, ok := decodeRepairOrder(event)
	if !ok {
		return nil
	}

	garage, err := uc.garages.Get(ctx, event.GarageID)
	if err != nil {
		return toAppError("start repair order", err)
	}

	if garage.StockIssueMode != "on_start" {
		return nil
	}

	return uc.postIfDraft(ctx, event.GarageID, payload.RepairOrderID)
}

// HandleRepairOrderClosed is the repair_order.closed consumer (FR-INV-02,
// FR-INV-20): posts the order's draft stock issue, idempotently. An order
// that has no issue yet (the created event was lost, or it predates the
// issue) gets one from the closing event's part lines first. A line whose
// part no longer resolves is skipped, so one bad line never blocks the rest.
func (uc *InventoryUseCase) HandleRepairOrderClosed(ctx context.Context, event outbox.Event) error {
	payload, ok := decodeRepairOrder(event)
	if !ok {
		return nil
	}

	if _, err := uc.CreateStockIssue(ctx, payload.request(event.GarageID)); err != nil {
		slog.Warn("inventory: creating the stock issue of a closed repair order failed",
			"repair_order_id", payload.RepairOrderID, "error", err)

		return err
	}

	return uc.postIfDraft(ctx, event.GarageID, payload.RepairOrderID)
}

func isNotFound(err error) bool {
	return errors.Is(err, domain.ErrStockIssueNotFound)
}
