-- Tenant/Garage Management (Database Design 2.3): the users working in a
-- garage. What a member may do comes from their staff groups' roles
-- (000010), not from a column here.

CREATE TYPE staff_status AS ENUM ('active', 'inactive');

CREATE TABLE garage_staff (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status staff_status NOT NULL DEFAULT 'active',
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_garage_staff_garage_id ON garage_staff (garage_id);
CREATE INDEX idx_garage_staff_user_id ON garage_staff (user_id);
CREATE INDEX idx_garage_staff_status ON garage_staff (status);

-- One membership per user and garage
CREATE UNIQUE INDEX idx_garage_staff_garage_user_unique ON garage_staff (garage_id, user_id);

-- Row-Level Security; the policies are created in 000013.
ALTER TABLE garage_staff ENABLE ROW LEVEL SECURITY;
ALTER TABLE garage_staff FORCE ROW LEVEL SECURITY;
