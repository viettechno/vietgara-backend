// Package handler registers the Notification module's event consumers
// (FRD 3.14). It has no RPCs of its own -- every e-mail is triggered by
// consuming an event another module already publishes.
package handler

import (
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// ConsumerName identifies this module's consumer in processed_events.
const ConsumerName = "notification"

// Subscribe registers the module's event consumers (Event Design Section
// 4, FR-NOT-04): a registration-confirmation e-mail once a user
// verifies its address, and a license renewal reminder ahead of a
// subscription's period end.
func Subscribe(bus outbox.Subscriber, processor *outbox.Processor, uc *usecase.NotificationUseCase) {
	bus.Subscribe("user.email_verified", processor.Wrap(ConsumerName, uc.HandleUserEmailVerified))
	bus.Subscribe("subscription.expiring_soon", processor.Wrap(ConsumerName, uc.HandleSubscriptionExpiringSoon))
	bus.Subscribe("subscription.status_changed", processor.Wrap(ConsumerName, uc.HandleSubscriptionStatusChanged))
	bus.Subscribe("subscription.payment_submitted", processor.Wrap(ConsumerName, uc.HandlePaymentSubmitted))
	bus.Subscribe("subscription.payment_approved", processor.Wrap(ConsumerName, uc.HandlePaymentApproved))
	bus.Subscribe("subscription.payment_rejected", processor.Wrap(ConsumerName, uc.HandlePaymentRejected))
	bus.Subscribe("subscription.payment_review_reminder", processor.Wrap(ConsumerName, uc.HandlePaymentReviewReminder))
}
