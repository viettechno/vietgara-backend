-- Create enum type for garage status
CREATE TYPE garage_status AS ENUM ('active', 'inactive', 'suspended');

-- Create garages table
CREATE TABLE garages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    tax_code VARCHAR(20),
    address TEXT,
    logo_url VARCHAR(500),
    timezone VARCHAR(20) NOT NULL DEFAULT 'UTC',
    currency VARCHAR(3) NOT NULL DEFAULT 'VND',
    status garage_status NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Create indexes on foreign keys
CREATE INDEX idx_garages_account_id ON garages (account_id);
CREATE INDEX idx_garages_status ON garages (status);

-- Create trigger function to automatically update updated_at
CREATE OR REPLACE FUNCTION update_garages_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger
CREATE TRIGGER trigger_garages_updated_at
BEFORE UPDATE ON garages
FOR EACH ROW
EXECUTE FUNCTION update_garages_updated_at();

-- Enable Row-Level Security
ALTER TABLE garages ENABLE ROW LEVEL SECURITY;

-- CRITICAL: Force RLS so table owners/app users cannot bypass it
ALTER TABLE garages FORCE ROW LEVEL SECURITY;

-- Create Row-Level Security policy for garages
-- The policy checks if the garage_id matches the session variable app.current_garage_id
CREATE POLICY garages_rls_policy ON garages
FOR ALL
USING (
    id = current_setting('app.current_garage_id', true)::uuid
)
WITH CHECK (
    id = current_setting('app.current_garage_id', true)::uuid
);

-- Add comment explaining RLS policy
COMMENT ON POLICY garages_rls_policy ON garages IS 
'Row-Level Security policy that restricts access based on app.current_garage_id session variable. 
Must be set before querying garages table: SELECT set_config(''app.current_garage_id'', garage_id::text, false)';
