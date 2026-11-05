package handler

import (
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

// ConsumerName identifies this module's consumer in processed_events.
const ConsumerName = "receivable"

// Subscribe registers the module's event consumers (Event Design Section 4):
// the receivable of a settlement follows its creation and the vouchers issued
// and voided against it.
func Subscribe(bus outbox.Subscriber, processor *outbox.Processor, uc *usecase.ReceivableUseCase) {
	h := processor.Wrap(ConsumerName, uc.HandleSettlementChanged)

	bus.Subscribe("settlement.created", h)
	bus.Subscribe("payment_voucher.issued", h)
	bus.Subscribe("payment_voucher.voided", h)
}
