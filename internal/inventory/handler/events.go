package handler

import (
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// ConsumerName identifies this module's consumer in processed_events.
const ConsumerName = "inventory"

// Subscribe registers the module's event consumers (Event Design Section 4):
// a new repair order gets its draft stock issue, which reserves its parts;
// the stock issue posts when the order starts (garage setting) or closes
// (FR-INV-02, FR-INV-20).
func Subscribe(bus outbox.Subscriber, processor *outbox.Processor, uc *usecase.InventoryUseCase) {
	bus.Subscribe("repair_order.created", processor.Wrap(ConsumerName, uc.HandleRepairOrderCreated))
	bus.Subscribe("repair_order.started", processor.Wrap(ConsumerName, uc.HandleRepairOrderStarted))
	bus.Subscribe("repair_order.closed", processor.Wrap(ConsumerName, uc.HandleRepairOrderClosed))
}
