-- Customer <-> user links (FR-CUS-06, FR-CAPP-05). A customer profile
-- belongs to one garage and needs no user; a garage can ask the user that
-- owns the profile's verified e-mail to link, and the user confirms with a
-- one-time code the garage enters, or accepts in the Customer App. The
-- garage never learns which user it is.
--
-- A customer has at most one current link (requested or linked); ended
-- links (declined, unlinked) stay as history and are never re-linked by
-- the verified-e-mail match, so a user's refusal is respected.
CREATE TYPE customer_user_link_status AS ENUM ('requested', 'linked', 'declined', 'unlinked');
CREATE TYPE customer_user_link_channel AS ENUM ('otp', 'app', 'auto_email');

CREATE TABLE customer_user_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    customer_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status customer_user_link_status NOT NULL DEFAULT 'requested',
    -- How the link was confirmed; set once it is linked.
    channel customer_user_link_channel,
    requested_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    linked_at TIMESTAMP WITH TIME ZONE,
    ended_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT customer_user_links_customer_fkey FOREIGN KEY (garage_id, customer_id)
        REFERENCES customers (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT customer_user_links_linked_check CHECK (status <> 'linked' OR (channel IS NOT NULL AND linked_at IS NOT NULL))
);

-- At most one current link per customer.
CREATE UNIQUE INDEX idx_customer_user_links_current ON customer_user_links (customer_id)
    WHERE status IN ('requested', 'linked');
CREATE INDEX idx_customer_user_links_customer ON customer_user_links (garage_id, customer_id, created_at DESC);
CREATE INDEX idx_customer_user_links_user ON customer_user_links (user_id, status);

ALTER TABLE customer_user_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_user_links FORCE ROW LEVEL SECURITY;

-- The garage reads and writes its own customers' links; a user reads and
-- answers the links addressed to them, in whichever garage.
CREATE POLICY customer_user_links_rls_policy ON customer_user_links
FOR ALL
USING (garage_id = app_current_garage_id() OR user_id = app_current_user_id())
WITH CHECK (garage_id = app_current_garage_id() OR user_id = app_current_user_id());

COMMENT ON POLICY customer_user_links_rls_policy ON customer_user_links IS
'The transaction''s garage, or the link''s own user (app.current_user_id, see app_current_user_id()).';

CREATE TRIGGER trigger_customer_user_links_row_audit BEFORE INSERT OR UPDATE ON customer_user_links
    FOR EACH ROW EXECUTE FUNCTION set_row_audit();
