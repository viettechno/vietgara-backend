package handler

import (
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// ConsumerName identifies this module's consumer in processed_events.
const ConsumerName = "license"

// Subscribe registers the module's event consumers (Event Design Section 4):
// every verified owner starts a trial (FR-LIC-01).
func Subscribe(bus outbox.Subscriber, processor *outbox.Processor, subscriptions *usecase.SubscriptionUseCase) {
	bus.Subscribe(usecase.EventUserEmailVerified, processor.Wrap(ConsumerName, subscriptions.HandleUserEmailVerified))
}
