-- Create enum type for garage_staff role
CREATE TYPE staff_role AS ENUM ('owner', 'accountant', 'technician', 'service_advisor', 'inventory_staff');

-- Create enum type for staff status
CREATE TYPE staff_status AS ENUM ('active', 'inactive', 'suspended');

-- Create garage_staff table
CREATE TABLE garage_staff (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    role staff_role NOT NULL,
    status staff_status NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Create indexes on foreign keys
CREATE INDEX idx_garage_staff_garage_id ON garage_staff (garage_id);
CREATE INDEX idx_garage_staff_account_id ON garage_staff (account_id);
CREATE INDEX idx_garage_staff_role ON garage_staff (role);
CREATE INDEX idx_garage_staff_status ON garage_staff (status);

-- Create unique constraint to prevent duplicate staff assignments
CREATE UNIQUE INDEX idx_garage_staff_garage_account_unique ON garage_staff (garage_id, account_id);

-- Create trigger function to automatically update updated_at
CREATE OR REPLACE FUNCTION update_garage_staff_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger
CREATE TRIGGER trigger_garage_staff_updated_at
BEFORE UPDATE ON garage_staff
FOR EACH ROW
EXECUTE FUNCTION update_garage_staff_updated_at();

-- Enable Row-Level Security
ALTER TABLE garage_staff ENABLE ROW LEVEL SECURITY;

-- CRITICAL: Force RLS so table owners/app users cannot bypass it
ALTER TABLE garage_staff FORCE ROW LEVEL SECURITY;

-- Create Row-Level Security policy for garage_staff
-- The policy checks if the garage_id matches the session variable app.current_garage_id
CREATE POLICY garage_staff_rls_policy ON garage_staff
FOR ALL
USING (
    garage_id = current_setting('app.current_garage_id', true)::uuid
)
WITH CHECK (
    garage_id = current_setting('app.current_garage_id', true)::uuid
);

-- Add comment explaining RLS policy
COMMENT ON POLICY garage_staff_rls_policy ON garage_staff IS 
'Row-Level Security policy that restricts access based on app.current_garage_id session variable. 
Must be set before querying garage_staff table: SELECT set_config(''app.current_garage_id'', garage_id::text, false)';
