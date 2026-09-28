-- Tenant/Garage Management (Database Design 2.3): the accounts working in a
-- garage. What a member may do comes from their staff groups' roles
-- (000010), not from a column here.

CREATE TYPE staff_status AS ENUM ('active', 'inactive');

CREATE TABLE garage_staff (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    status staff_status NOT NULL DEFAULT 'active',
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_garage_staff_garage_id ON garage_staff (garage_id);
CREATE INDEX idx_garage_staff_account_id ON garage_staff (account_id);
CREATE INDEX idx_garage_staff_status ON garage_staff (status);

-- One membership per account and garage
CREATE UNIQUE INDEX idx_garage_staff_garage_account_unique ON garage_staff (garage_id, account_id);

CREATE OR REPLACE FUNCTION update_garage_staff_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_garage_staff_updated_at
BEFORE UPDATE ON garage_staff
FOR EACH ROW
EXECUTE FUNCTION update_garage_staff_updated_at();

-- Row-Level Security; the policies are created in 000013.
ALTER TABLE garage_staff ENABLE ROW LEVEL SECURITY;
ALTER TABLE garage_staff FORCE ROW LEVEL SECURITY;
