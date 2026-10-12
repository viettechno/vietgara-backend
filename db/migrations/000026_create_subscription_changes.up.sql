-- Plan changes (FR-LIC-02): a plan the owner chose that has not taken effect
-- yet. An upgrade and a downgrade both take effect when the current period
-- ends (when the trial ends, during the trial); a past-due or suspended
-- owner switches at once and has no pending change. At most one change is
-- pending per subscription: choosing another plan replaces it, and the
-- owner can cancel it. A change that is due while the new plan's limits are
-- exceeded is postponed and the owner is told.
CREATE TYPE plan_change_status AS ENUM ('scheduled', 'postponed', 'applied', 'cancelled');

CREATE TABLE subscription_changes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id UUID NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    plan_id UUID NOT NULL REFERENCES plans(id),
    status plan_change_status NOT NULL DEFAULT 'scheduled',
    -- When it applies: the end of the period it was chosen in. An admin's
    -- extension of the period moves it.
    effective_at TIMESTAMP WITH TIME ZONE NOT NULL,
    requested_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    postponed_at TIMESTAMP WITH TIME ZONE,
    applied_at TIMESTAMP WITH TIME ZONE,
    cancelled_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- At most one pending change per subscription.
CREATE UNIQUE INDEX idx_subscription_changes_pending ON subscription_changes (subscription_id)
    WHERE status IN ('scheduled', 'postponed');
-- The expiry job's scan of due changes.
CREATE INDEX idx_subscription_changes_due ON subscription_changes (effective_at)
    WHERE status IN ('scheduled', 'postponed');

CREATE TRIGGER trigger_subscription_changes_row_audit BEFORE INSERT OR UPDATE ON subscription_changes
    FOR EACH ROW EXECUTE FUNCTION set_row_audit();
