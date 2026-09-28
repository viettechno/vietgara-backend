-- staff_invitations (FR-TEN-04): only existing accounts can be invited; the
-- invitee is e-mailed and joins the given groups on acceptance. One pending
-- invitation per garage and account; invitations expire after 7 days.

CREATE TYPE invitation_status AS ENUM ('pending', 'accepted', 'declined', 'revoked', 'expired');

CREATE TABLE staff_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    group_ids UUID[] NOT NULL DEFAULT '{}',
    status invitation_status NOT NULL DEFAULT 'pending',
    invited_by UUID NOT NULL REFERENCES accounts(id),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    responded_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_staff_invitations_pending_unique ON staff_invitations (garage_id, account_id) WHERE status = 'pending';
CREATE INDEX idx_staff_invitations_account_id ON staff_invitations (account_id, created_at DESC);
CREATE INDEX idx_staff_invitations_garage_id ON staff_invitations (garage_id, created_at DESC);
CREATE TRIGGER trigger_staff_invitations_updated_at BEFORE UPDATE ON staff_invitations FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Row-Level Security; the policies are created in 000013.
ALTER TABLE staff_invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE staff_invitations FORCE ROW LEVEL SECURITY;
