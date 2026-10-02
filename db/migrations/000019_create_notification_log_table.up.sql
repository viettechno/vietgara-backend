-- Notification Service (HLD module #14), Phase 1 email channel only
-- (FR-NOT-04: registration confirmation, license renewal reminders).
-- Append-only audit of what was sent -- the Outbox Pattern's
-- processed_events already gives exactly-once delivery per consumer, so
-- this table is for visibility/debugging, not deduplication. Account-level
-- like plans/subscriptions/entitlements (Database Design 6.3): no garage
-- scope, no RLS.

CREATE TYPE notification_channel AS ENUM ('email');
CREATE TYPE notification_status AS ENUM ('sent', 'failed');

CREATE TABLE notification_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID,
    channel notification_channel NOT NULL,
    template VARCHAR(100) NOT NULL,
    recipient VARCHAR(255) NOT NULL,
    status notification_status NOT NULL,
    error TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notification_log_account ON notification_log (account_id, created_at DESC) WHERE account_id IS NOT NULL;

-- Append-only: the application role may only INSERT and SELECT, the same
-- restriction audit_log already has (migration 000013).
REVOKE UPDATE, DELETE ON notification_log FROM vietgara_app;
