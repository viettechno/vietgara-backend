package handler

import (
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

// ConsumerName identifies this module's consumer in processed_events.
const ConsumerName = "tenant"

// Subscribe registers the module's event consumers (Event Design Section 4):
// every verified owner gets the default roles (FR-IAM-03).
func Subscribe(bus outbox.Subscriber, processor *outbox.Processor, uc *usecase.TenantUseCase) {
	bus.Subscribe("account.email_verified", processor.Wrap(ConsumerName, uc.HandleAccountEmailVerified))
}
