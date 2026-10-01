package handler

import (
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// ConsumerName identifies this module's consumer in processed_events.
const ConsumerName = "inventory"

// Subscribe registers the module's event consumers (Event Design Section 4):
// a closed repair order's part lines deduct stock (FR-INV-02).
func Subscribe(bus outbox.Subscriber, processor *outbox.Processor, uc *usecase.InventoryUseCase) {
	bus.Subscribe("repair_order.closed", processor.Wrap(ConsumerName, uc.HandleRepairOrderClosed))
}
