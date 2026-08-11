package usecase

import "context"

// EventPublisher is the minimal interface usecases in this module need to
// publish domain events via the Outbox Pattern. The concrete implementation
// (writing to the outbox_events table + a background relay worker
// publishing to Cloud Pub/Sub) lives in internal/platform/outbox -- not
// shown here, see Event Design Section 2 for the full pattern.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID string, payload any) error
}
