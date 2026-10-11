-- Supplier module (FR-SUP-01): the garage's directory of parts suppliers.
-- A supplier is not a customer. Insurers are not suppliers either; they
-- come back in their own table in the Insurance module (Phase 17).

-- Create suppliers table
-- contact_info holds {"contact_person", "phone", "email", "address"};
-- bank_details holds {"bank_name", "account_number", "account_holder"}.
-- Rows are soft-deleted (deleted_at) so documents that name a supplier
-- keep working.
CREATE TABLE suppliers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    tax_code VARCHAR(20),
    contact_info JSONB NOT NULL DEFAULT '{}'::jsonb,
    bank_details JSONB NOT NULL DEFAULT '{}'::jsonb,
    note TEXT,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE
);

-- Listing of a garage's live suppliers
CREATE INDEX idx_suppliers_garage ON suppliers (garage_id) WHERE deleted_at IS NULL;

-- One live supplier per name within a garage (case-insensitive)
CREATE UNIQUE INDEX idx_suppliers_garage_name_unique ON suppliers (garage_id, lower(name)) WHERE deleted_at IS NULL;

-- Create trigger function to automatically update updated_at
CREATE OR REPLACE FUNCTION update_suppliers_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger
CREATE TRIGGER trigger_suppliers_updated_at
BEFORE UPDATE ON suppliers
FOR EACH ROW
EXECUTE FUNCTION update_suppliers_updated_at();

-- Enable Row-Level Security
ALTER TABLE suppliers ENABLE ROW LEVEL SECURITY;

-- CRITICAL: Force RLS so table owners/app users cannot bypass it
ALTER TABLE suppliers FORCE ROW LEVEL SECURITY;

-- Create Row-Level Security policy for suppliers
CREATE POLICY suppliers_rls_policy ON suppliers
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY suppliers_rls_policy ON suppliers IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
