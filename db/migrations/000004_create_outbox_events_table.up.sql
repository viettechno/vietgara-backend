-- Create outbox_events table for the Outbox Pattern (Event Design Section 2).
-- Writes happen inside the same transaction as the business data change;
-- a background relay worker polls 'pending' rows and publishes them to
-- Cloud Pub/Sub, then marks them 'published'.
CREATE TABLE outbox_events (
    id UUID PRIMARY KEY,
    event_type VARCHAR(255) NOT NULL,
    garage_id UUID NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Index for the relay worker's pending-row polling loop
CREATE INDEX idx_outbox_events_status ON outbox_events (status);