-- Tenant/Garage Management (Database Design 2.2): one garage = one tenant;
-- an owner account has many garages (FR-TEN-01/02/05).

-- ---------------------------------------------------------------------------
-- Row-Level Security helpers: the tenant (garage) and caller (account) of
-- the current transaction, set by the application with
-- set_config(..., true). An unset or empty setting is NULL, which matches
-- nothing.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION app_current_garage_id() RETURNS UUID
LANGUAGE sql STABLE AS $$ SELECT NULLIF(current_setting('app.current_garage_id', true), '')::uuid $$;

CREATE OR REPLACE FUNCTION app_current_account_id() RETURNS UUID
LANGUAGE sql STABLE AS $$ SELECT NULLIF(current_setting('app.current_account_id', true), '')::uuid $$;

CREATE TYPE garage_status AS ENUM ('active', 'inactive');

-- operating_hours: [{"day": 1..7 (Monday..Sunday), "closed", "open", "close"}].
-- The numbering templates use {YYYY}, {YY}, {MM}, {DD} and one {SEQ:n}.
CREATE TABLE garages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    tax_code VARCHAR(20),
    address TEXT,
    phone VARCHAR(20),
    email VARCHAR(255),
    logo_url VARCHAR(500),
    timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Ho_Chi_Minh',
    currency VARCHAR(3) NOT NULL DEFAULT 'VND',
    status garage_status NOT NULL DEFAULT 'active',
    operating_hours JSONB NOT NULL DEFAULT '[
        {"day": 1, "closed": false, "open": "08:00", "close": "17:30"},
        {"day": 2, "closed": false, "open": "08:00", "close": "17:30"},
        {"day": 3, "closed": false, "open": "08:00", "close": "17:30"},
        {"day": 4, "closed": false, "open": "08:00", "close": "17:30"},
        {"day": 5, "closed": false, "open": "08:00", "close": "17:30"},
        {"day": 6, "closed": false, "open": "08:00", "close": "12:00"},
        {"day": 7, "closed": true, "open": "", "close": ""}
    ]'::jsonb,
    quote_number_template VARCHAR(50) NOT NULL DEFAULT 'BG-{YYYY}{MM}-{SEQ:4}',
    repair_order_number_template VARCHAR(50) NOT NULL DEFAULT 'LSC-{YYYY}{MM}-{SEQ:4}',
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_garages_account_id ON garages (account_id);
CREATE INDEX idx_garages_status ON garages (status);

CREATE OR REPLACE FUNCTION update_garages_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_garages_updated_at
BEFORE UPDATE ON garages
FOR EACH ROW
EXECUTE FUNCTION update_garages_updated_at();

-- Row-Level Security. FORCE binds the table owner too. The policies also
-- admit the caller's own memberships and invitations, so they are created
-- in 000013 once every table they reference exists; until then no row is
-- visible to a non-bypass role.
ALTER TABLE garages ENABLE ROW LEVEL SECURITY;
ALTER TABLE garages FORCE ROW LEVEL SECURITY;
