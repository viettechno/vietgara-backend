-- Read-only support access (Release 8.2, FR-IAM-11).
--
-- A support grant lets one admin staff member read one garage's data for a
-- limited time. The garage owner approves it; a Platform Admin's grant is
-- active at once (approved_by stays NULL, granted_by_platform_admin is set).
-- The session length is kept as ttl_seconds and counted from the approval,
-- so a request that waits does not eat the session. A request nobody answers
-- lapses 7 days after it was made. Its status is never stored: pending,
-- active, expired and revoked follow from these columns and the clock.
--
-- Every read made under a grant is appended to support_access_events (shown
-- to the owner) and to audit_log.

CREATE TABLE support_access_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    staff_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason TEXT NOT NULL,
    scope VARCHAR(20) NOT NULL DEFAULT 'read_only' CHECK (scope = 'read_only'),
    ttl_seconds INTEGER NOT NULL CHECK (ttl_seconds BETWEEN 3600 AND 604800),
    requested_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    approved_by UUID REFERENCES users(id),
    approved_at TIMESTAMP WITH TIME ZONE,
    expires_at TIMESTAMP WITH TIME ZONE,
    revoked_at TIMESTAMP WITH TIME ZONE,
    granted_by_platform_admin BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID,
    CONSTRAINT support_access_grants_approval CHECK ((approved_at IS NULL) = (expires_at IS NULL))
);
CREATE INDEX idx_support_access_grants_garage ON support_access_grants (garage_id, requested_at DESC);
CREATE INDEX idx_support_access_grants_staff ON support_access_grants (staff_user_id, requested_at DESC);
-- The lookup of every support read: the open grants of one staff member in one garage.
CREATE INDEX idx_support_access_grants_open ON support_access_grants (garage_id, staff_user_id) WHERE revoked_at IS NULL;

-- The owner's side is scoped to the garage, the staff's side to the user (a
-- request is made before any garage is in scope).
ALTER TABLE support_access_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_access_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY support_access_grants_select ON support_access_grants FOR SELECT
    USING (garage_id = app_current_garage_id() OR staff_user_id = app_current_user_id());
CREATE POLICY support_access_grants_insert ON support_access_grants FOR INSERT
    WITH CHECK (staff_user_id = app_current_user_id());
CREATE POLICY support_access_grants_update ON support_access_grants FOR UPDATE
    USING (garage_id = app_current_garage_id() OR staff_user_id = app_current_user_id())
    WITH CHECK (garage_id = app_current_garage_id() OR staff_user_id = app_current_user_id());

CREATE TABLE support_access_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    grant_id UUID NOT NULL REFERENCES support_access_grants(id) ON DELETE CASCADE,
    garage_id UUID NOT NULL,
    staff_user_id UUID NOT NULL,
    -- The API call (e.g. GetSettlement) and the entity it named; entity_id is
    -- empty for a list.
    method VARCHAR(100) NOT NULL,
    entity_type VARCHAR(50) NOT NULL,
    entity_id VARCHAR(100) NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID
);
CREATE INDEX idx_support_access_events_garage ON support_access_events (garage_id, created_at DESC);
CREATE INDEX idx_support_access_events_grant ON support_access_events (grant_id, created_at DESC);

ALTER TABLE support_access_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_access_events FORCE ROW LEVEL SECURITY;
CREATE POLICY support_access_events_select ON support_access_events FOR SELECT
    USING (garage_id = app_current_garage_id());
CREATE POLICY support_access_events_insert ON support_access_events FOR INSERT
    WITH CHECK (staff_user_id = app_current_user_id());
-- Append-only, like audit_log.
REVOKE UPDATE, DELETE ON support_access_events FROM vietgara_app;

-- Admin staff search garages by name and owner before they hold a grant, so
-- they see no garage through the policies above. The support module turns
-- this setting on for its directory queries only (transaction-local) and
-- selects the name and owner, nothing else; the platform permission
-- garage_directory.read guards the endpoint.
CREATE POLICY garages_select_directory ON garages FOR SELECT
    USING (current_setting('app.garage_directory', true) = 'on');

CREATE TRIGGER trigger_support_access_grants_row_audit BEFORE INSERT OR UPDATE ON support_access_grants FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_support_access_events_created_audit BEFORE INSERT ON support_access_events FOR EACH ROW EXECUTE FUNCTION set_created_audit();
