-- Supplier module (FR-SUP-01): the garage's directory of parts suppliers.
-- A supplier is not a customer. Insurers are not suppliers either; they
-- come back in their own table in the Insurance module (Phase 17).

-- Create suppliers table
-- contact_info holds {"contact_person", "phone", "email", "address"};
-- bank_details holds {"bank_name", "account_number", "account_holder"}.
-- A supplier that documents refer to is deactivated (is_active = false,
-- FR-CAT-07), not deleted, so those documents keep working; only an
-- unreferenced supplier is soft-deleted (deleted_at).
CREATE TABLE suppliers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    tax_code VARCHAR(20),
    contact_info JSONB NOT NULL DEFAULT '{}'::jsonb,
    bank_details JSONB NOT NULL DEFAULT '{}'::jsonb,
    note TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    -- Target of the (garage_id, supplier_id) foreign keys of goods receipt
    -- lines and part suppliers (Release 7.0).
    CONSTRAINT suppliers_garage_id_id_key UNIQUE (garage_id, id)
);

-- Listing of a garage's live suppliers
CREATE INDEX idx_suppliers_garage ON suppliers (garage_id) WHERE deleted_at IS NULL;

-- One live supplier per name within a garage (case-insensitive)
CREATE UNIQUE INDEX idx_suppliers_garage_name_unique ON suppliers (garage_id, lower(name)) WHERE deleted_at IS NULL;

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
