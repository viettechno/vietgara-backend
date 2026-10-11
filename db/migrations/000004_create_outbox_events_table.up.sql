-- Outbox Pattern (Event Design Section 2): events are written in the same
-- transaction as the business change; the relay publishes pending rows to
-- Kafka and marks them published, counting failed deliveries. An event
-- belongs to a garage, or to a user for user-level events.
CREATE TYPE outbox_status AS ENUM ('pending', 'published', 'failed');

CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    event_type VARCHAR(255) NOT NULL,
    garage_id UUID,
    user_id UUID,
    payload JSONB NOT NULL,
    status outbox_status NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    published_at TIMESTAMP WITH TIME ZONE,
    created_by UUID
);

-- The relay's polling loop: oldest pending events first
CREATE INDEX idx_outbox_events_pending ON outbox_events (created_at) WHERE status = 'pending';
