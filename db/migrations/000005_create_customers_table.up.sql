-- Customer & Vehicle module (HLD module #4), Database Design Section 3.1.
-- Trigram indexes back the fuzzy customer search (Database Design Section 8).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Create customers table
-- phone is stored normalized to E.164 ("+84901234567", pkg/phone) by the
-- application so the per-garage uniqueness check below is meaningful and
-- a customer's phone matches a user's verified phone (FR-CUS-06).
-- consent_log is an append-only JSON array of personal-data consent
-- decisions (Decree 13/2023/ND-CP): [{"granted", "recorded_at", "recorded_by"}].
-- A customer that quotes or repair orders refer to is not deleted but
-- deactivated (is_active = false, FR-CAT-07): it leaves pickers and staff
-- can reactivate it. Only an unreferenced customer is soft-deleted
-- (deleted_at).
CREATE TABLE customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    full_name VARCHAR(255) NOT NULL,
    phone VARCHAR(20),
    email VARCHAR(255),
    address TEXT,
    consent_log JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    -- Target of the vehicles (garage_id, customer_id) foreign key, which
    -- guarantees a vehicle and its owner always belong to the same garage.
    CONSTRAINT customers_garage_id_id_key UNIQUE (garage_id, id)
);

-- Newest-first listing of live customers
CREATE INDEX idx_customers_garage_created_at ON customers (garage_id, created_at DESC) WHERE deleted_at IS NULL;

-- One live customer per phone number within a garage
CREATE UNIQUE INDEX idx_customers_garage_phone_unique ON customers (garage_id, phone) WHERE deleted_at IS NULL AND phone IS NOT NULL;

-- Fuzzy search on name, phone and email
CREATE INDEX idx_customers_full_name_trgm ON customers USING gin (full_name gin_trgm_ops);
CREATE INDEX idx_customers_phone_trgm ON customers USING gin (phone gin_trgm_ops);
CREATE INDEX idx_customers_email_trgm ON customers USING gin (email gin_trgm_ops);

-- Enable Row-Level Security
ALTER TABLE customers ENABLE ROW LEVEL SECURITY;

-- CRITICAL: Force RLS so table owners/app users cannot bypass it
ALTER TABLE customers FORCE ROW LEVEL SECURITY;

-- Create Row-Level Security policy for customers
CREATE POLICY customers_rls_policy ON customers
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY customers_rls_policy ON customers IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
