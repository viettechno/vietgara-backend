package handler

import (
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
)

// ConsumerName identifies this module's consumer in processed_events.
const ConsumerName = "repairorder"

// Subscribe registers the module's event consumers (Event Design Section 4):
// Inventory tells when the draft stock issue of an order cannot be covered by
// the stock on hand, which makes the order wait on parts (FR-INV-15).
func Subscribe(bus outbox.Subscriber, processor *outbox.Processor, uc *usecase.RepairOrderUseCase) {
	bus.Subscribe("stock_issue.shortage_changed", processor.Wrap(ConsumerName, uc.HandleStockShortage))
}
