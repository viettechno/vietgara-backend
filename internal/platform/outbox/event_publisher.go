// Package outbox implements the Outbox Pattern (Event Design Section 2):
// domain events are written to outbox_events inside the same transaction
// as the business data change, then published to Kafka (ADR-004) -- or,
// without brokers configured, to in-process subscribers -- and marked
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

// Publish inserts eventType with the marshalled payload into the outbox.
// garageID and/or userID name what the event is about: the garage it
// belongs to (tenant events) or the user (identity and license events);
// the first non-empty one is the Kafka partition key. It runs on the
// querier carried in ctx, so the row lands in the caller's transaction.
func (p *EventPublisher) Publish(ctx context.Context, eventType, garageID, userID string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	q := platformdb.QuerierFromContext(ctx, p.pool)
	_, err = q.Exec(ctx,
		`INSERT INTO outbox_events (id, event_type, garage_id, user_id, payload, status)
		 VALUES ($1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, 'pending')`,
		uuid.NewString(), eventType, garageID, userID, body)

	return err
}
