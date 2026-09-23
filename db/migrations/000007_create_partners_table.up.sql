-- Customer & Vehicle module (HLD module #4), Database Design Section 3.3:
-- the garage's directory of parts suppliers and insurers (FR-CUS-04).

-- Create enum type for partner type
CREATE TYPE partner_type AS ENUM ('supplier', 'insurer');

-- Create partners table
-- contact_info holds {"contact_person", "phone", "email", "address"}.
CREATE TABLE partners (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    type partner_type NOT NULL,
    name VARCHAR(255) NOT NULL,
    tax_code VARCHAR(20),
    contact_info JSONB NOT NULL DEFAULT '{}'::jsonb,
    note TEXT,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE
);

-- Indexes
CREATE INDEX idx_partners_garage_type ON partners (garage_id, type) WHERE deleted_at IS NULL;

-- One live partner per name and type within a garage (case-insensitive)
CREATE UNIQUE INDEX idx_partners_garage_type_name_unique ON partners (garage_id, type, lower(name)) WHERE deleted_at IS NULL;

-- Create trigger function to automatically update updated_at
CREATE OR REPLACE FUNCTION update_partners_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger
CREATE TRIGGER trigger_partners_updated_at
BEFORE UPDATE ON partners
FOR EACH ROW
EXECUTE FUNCTION update_partners_updated_at();

-- Enable Row-Level Security
ALTER TABLE partners ENABLE ROW LEVEL SECURITY;

-- CRITICAL: Force RLS so table owners/app users cannot bypass it
ALTER TABLE partners FORCE ROW LEVEL SECURITY;

-- Create Row-Level Security policy for partners
CREATE POLICY partners_rls_policy ON partners
FOR ALL
USING (
    garage_id = current_setting('app.current_garage_id', true)::uuid
)
WITH CHECK (
    garage_id = current_setting('app.current_garage_id', true)::uuid
);

COMMENT ON POLICY partners_rls_policy ON partners IS
'Row-Level Security policy that restricts access based on app.current_garage_id session variable.';
