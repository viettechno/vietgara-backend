package outbox

import (
	"context"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Processor gives event handlers exactly-once effects on top of
// at-least-once delivery (Event Design Section 6): each (consumer, event)
// pair is recorded in processed_events in the same transaction as the
// handler's changes, and redeliveries are skipped.
type Processor struct {
	pool *platformdb.Pool
	tx   Transactor
}

// NewProcessor returns a Processor on the given pool.
func NewProcessor(pool *platformdb.Pool, tx Transactor) *Processor {
	return &Processor{pool: pool, tx: tx}
}

// Wrap returns h guarded by processed_events under consumer's name. The
// handler runs in a transaction scoped to the event's garage and user.
func (p *Processor) Wrap(consumer string, h Handler) Handler {
	return func(ctx context.Context, event Event) error {
		scope := platformdb.Scope{GarageID: event.GarageID, UserID: event.UserID}

		return p.tx.WithTransaction(ctx, scope, func(ctx context.Context) error {
			tag, err := platformdb.QuerierFromContext(ctx, p.pool).Exec(ctx,
				`INSERT INTO processed_events (consumer, event_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				consumer, event.ID)
			if err != nil {
				return err
			}

			if tag.RowsAffected() == 0 {
				return nil // already processed
			}

			return h(ctx, event)
		})
	}
}
