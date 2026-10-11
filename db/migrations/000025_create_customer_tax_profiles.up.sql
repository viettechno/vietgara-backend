-- Customer tax profiles (FR-CUS-07): the legal identities a customer can be
-- billed under. A customer has at most one personal profile and any number
-- of company profiles; one of them may be the default. A profile that a
-- document was billed to is not deleted but deactivated (FR-CAT-07), and a
-- document keeps its own snapshot of the profile it was billed to.
CREATE TYPE customer_tax_profile_kind AS ENUM ('personal', 'company');

CREATE TABLE customer_tax_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    customer_id UUID NOT NULL,
    kind customer_tax_profile_kind NOT NULL,
    -- The company's legal name, or the person's name.
    legal_name VARCHAR(255) NOT NULL,
    -- 10 digits, or 13 with the branch suffix.
    tax_id VARCHAR(13),
    address VARCHAR(500),
    -- Where an e-invoice is sent.
    invoice_email VARCHAR(255),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT customer_tax_profiles_customer_fkey FOREIGN KEY (garage_id, customer_id)
        REFERENCES customers (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT customer_tax_profiles_garage_id_id_key UNIQUE (garage_id, id),
    CONSTRAINT customer_tax_profiles_tax_id_check CHECK (tax_id IS NULL OR tax_id ~ '^[0-9]{10}([0-9]{3})?$')
);

-- At most one live personal profile, and one default, per customer.
CREATE UNIQUE INDEX idx_customer_tax_profiles_personal ON customer_tax_profiles (customer_id)
    WHERE kind = 'personal' AND deleted_at IS NULL;
CREATE UNIQUE INDEX idx_customer_tax_profiles_default ON customer_tax_profiles (customer_id)
    WHERE is_default AND deleted_at IS NULL;
CREATE INDEX idx_customer_tax_profiles_customer ON customer_tax_profiles (garage_id, customer_id, created_at)
    WHERE deleted_at IS NULL;

ALTER TABLE customer_tax_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_tax_profiles FORCE ROW LEVEL SECURITY;

CREATE POLICY customer_tax_profiles_rls_policy ON customer_tax_profiles
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY customer_tax_profiles_rls_policy ON customer_tax_profiles IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

CREATE TRIGGER trigger_customer_tax_profiles_row_audit BEFORE INSERT OR UPDATE ON customer_tax_profiles
    FOR EACH ROW EXECUTE FUNCTION set_row_audit();
