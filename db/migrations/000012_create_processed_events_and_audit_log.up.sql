-- Platform tables without Row-Level Security.

-- processed_events: Kafka delivers at least once; a consumer records each
-- handled event in the handler's transaction, so a redelivery is skipped
-- (Event Design 6).
CREATE TABLE processed_events (
    consumer VARCHAR(100) NOT NULL,
    event_id UUID NOT NULL,
    processed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (consumer, event_id)
);

-- audit_log: append-only record of security-relevant and financial changes
-- (Security Architecture 7). The application role may only INSERT and
-- SELECT (000013).
CREATE TABLE audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_account_id UUID,
    garage_id UUID,
    action VARCHAR(100) NOT NULL,
    entity_type VARCHAR(50) NOT NULL,
    entity_id VARCHAR(100) NOT NULL,
    before JSONB,
    after JSONB,
    ip VARCHAR(64),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_audit_log_entity ON audit_log (entity_type, entity_id, created_at DESC);
CREATE INDEX idx_audit_log_garage ON audit_log (garage_id, created_at DESC) WHERE garage_id IS NOT NULL;
