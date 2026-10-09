package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Relay defaults (Event Design Section 2: poll every few seconds).
const (
	DefaultRelayInterval  = 2 * time.Second
	DefaultRelayBatchSize = 100
	// DefaultMaxAttempts is how often delivery is retried before an event
	// is parked as 'failed' for manual follow-up (NFR-AV-05).
	DefaultMaxAttempts = 10

	// EnvelopeVersion is the version of the event envelope.
	EnvelopeVersion = 1
)

// Event is one outbox row in the standard envelope (Event Design Section 3).
type Event struct {
	ID         string          `json:"event_id"`
	Type       string          `json:"event_type"`
	OccurredAt time.Time       `json:"occurred_at"`
	GarageID   string          `json:"garage_id,omitempty"`
	UserID     string          `json:"user_id,omitempty"`
	Version    int             `json:"version"`
	Data       json.RawMessage `json:"data"`
}

// Key is the partition key: the garage, else the user.
func (e Event) Key() string {
	if e.GarageID != "" {
		return e.GarageID
	}

	return e.UserID
}

// Publisher delivers an event: the Kafka producer, or the in-process
// Dispatcher when no brokers are configured.
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// Transactor runs fn in a scoped transaction (see db.TxManager).
type Transactor interface {
	WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error
}

// RelayOption customizes a Relay.
type RelayOption func(*Relay)

// WithInterval sets how often the relay polls for pending events.
func WithInterval(interval time.Duration) RelayOption {
	return func(r *Relay) { r.interval = interval }
}

// WithMaxAttempts sets how many failed deliveries park an event as failed.
func WithMaxAttempts(attempts int) RelayOption {
	return func(r *Relay) { r.maxAttempts = attempts }
}

// Relay is the Outbox Relay worker (LLD Section 4.3): it polls pending
// outbox_events and hands each one to the Publisher.
//
// Each event is delivered in its own transaction scoped to the event's
// garage and user, holding a row lock (FOR UPDATE SKIP LOCKED, so
// several instances can run the relay) while the publisher runs. With
// Kafka the event is marked published once the broker acknowledged it; with
// the in-process Dispatcher the consumers write through the same
// transaction, so their changes commit atomically with the mark. A failure
// rolls back and the event is retried on the next tick (at-least-once).
type Relay struct {
	pool        *platformdb.Pool
	tx          Transactor
	publisher   Publisher
	interval    time.Duration
	batchSize   int
	maxAttempts int
}

// NewRelay wires a relay to the outbox table and a publisher.
func NewRelay(pool *platformdb.Pool, tx Transactor, publisher Publisher, opts ...RelayOption) *Relay {
	r := &Relay{
		pool: pool, tx: tx, publisher: publisher,
		interval: DefaultRelayInterval, batchSize: DefaultRelayBatchSize, maxAttempts: DefaultMaxAttempts,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Run relays pending events every interval until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		if _, err := r.RelayPending(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("outbox relay: listing pending events failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RelayPending delivers up to one batch of pending events, oldest first, and
// returns how many were published. Failed deliveries are recorded and do
// not stop the batch.
func (r *Relay) RelayPending(ctx context.Context) (int, error) {
	ids, err := r.pendingIDs(ctx)
	if err != nil {
		return 0, err
	}

	published := 0

	for _, id := range ids {
		delivered, err := r.deliver(ctx, id)
		if err != nil {
			slog.Warn("outbox relay: delivery failed, will retry", "event_id", id, "error", err)
			r.recordFailure(ctx, id, err)

			continue
		}

		if delivered {
			published++
		}
	}

	return published, nil
}

func (r *Relay) pendingIDs(ctx context.Context) ([]string, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	rows, err := q.Query(ctx, `SELECT id FROM outbox_events WHERE status = 'pending' ORDER BY created_at LIMIT $1`, r.batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// deliver publishes one event and marks it published in the same
// transaction. It reports false when another relay holds or already
// delivered the event.
func (r *Relay) deliver(ctx context.Context, id string) (bool, error) {
	delivered := false

	err := r.tx.WithTransaction(ctx, platformdb.Scope{}, func(txCtx context.Context) error {
		q := platformdb.QuerierFromContext(txCtx, r.pool)
		event := Event{Version: EnvelopeVersion}

		err := q.QueryRow(txCtx, `
			SELECT id, event_type, COALESCE(garage_id::text, ''), COALESCE(user_id::text, ''), payload, created_at
			FROM outbox_events
			WHERE id = $1 AND status = 'pending' FOR UPDATE SKIP LOCKED`, id,
		).Scan(&event.ID, &event.Type, &event.GarageID, &event.UserID, &event.Data, &event.OccurredAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		if err != nil {
			return err
		}

		// In-process consumers run inside the event's scope, like a request.
		scope := platformdb.Scope{GarageID: event.GarageID, UserID: event.UserID}

		err = r.tx.WithTransaction(txCtx, scope, func(ctx context.Context) error {
			return r.publisher.Publish(ctx, event)
		})
		if err != nil {
			return err
		}

		if _, err := q.Exec(txCtx, `UPDATE outbox_events SET status = 'published', published_at = now() WHERE id = $1`, id); err != nil {
			return err
		}

		delivered = true

		return nil
	})

	return delivered, err
}

// recordFailure counts a failed delivery, parking the event as 'failed'
// once it reached maxAttempts.
func (r *Relay) recordFailure(ctx context.Context, id string, cause error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	_, err := q.Exec(ctx, `
		UPDATE outbox_events
		SET attempts = attempts + 1, last_error = $2,
			status = CASE WHEN attempts + 1 >= $3 THEN 'failed'::outbox_status ELSE status END
		WHERE id = $1 AND status = 'pending'`,
		id, cause.Error(), r.maxAttempts)
	if err != nil {
		slog.Error("outbox relay: recording a failed delivery failed", "event_id", id, "error", err)
	}
}
