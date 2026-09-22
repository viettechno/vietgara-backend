// Package outbox implements the Outbox Pattern (Event Design Section 2):
// domain events are written to outbox_events inside the same transaction
// as the business data change, then published to Cloud Pub/Sub and marked
// published by a background relay worker.
package outbox

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// EventPublisher writes domain events to the outbox_events table.
type EventPublisher struct {
	pool *platformdb.Pool
}

// NewEventPublisher returns an EventPublisher backed by the given pool.
func NewEventPublisher(pool *platformdb.Pool) *EventPublisher {
	return &EventPublisher{pool: pool}
}

// Publish inserts eventType for garageID with the marshalled payload into
// the outbox. It runs on the querier carried in ctx, so when the caller
// opened a transaction via db.TxManager.WithTransaction the row lands in
// the same transaction as the business change.
func (p *EventPublisher) Publish(ctx context.Context, eventType, garageID string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	q := platformdb.QuerierFromContext(ctx, p.pool)
	_, err = q.Exec(ctx,
		`INSERT INTO outbox_events (id, event_type, garage_id, payload, status) VALUES ($1, $2, $3, $4, 'pending')`,
		uuid.NewString(), eventType, garageID, body)

	return err
}
