-- Quote & Pricing module (HLD module #5), Database Design Section 4.1-4.2,
-- Release Plan 1.2 (FR-QUO-01..05).

CREATE TYPE quote_status AS ENUM ('draft', 'sent', 'approved', 'rejected', 'converted');
CREATE TYPE quote_item_type AS ENUM ('labor', 'part');
CREATE TYPE quote_line_decision AS ENUM ('pending', 'approved', 'rejected');

-- A quote's public_token_hash/public_token_expires_at back the customer
-- approval link (FR-QUO-02, QUOTE_APPROVAL_MODE_LINK): the plaintext token
-- is returned once by SendQuote and never stored; only its SHA-256 hash
-- is, so a leaked database dump cannot be used to forge a link. Cleared
-- once the quote is decided or the link expires. Rows are soft-deleted
-- (deleted_at) because vehicle history (FR-CUS-03) and, later, repair
-- orders keep referencing them.
CREATE TABLE quotes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    customer_id UUID NOT NULL,
    vehicle_id UUID NOT NULL,
    -- The customer's display name and the vehicle's license plate at
    -- creation/update time, denormalized so this module's queries never
    -- join the Customer module's tables directly (each module owns its
    -- own tables, HLD Section 2); kept current by the Quote use cases,
    -- which already read them from Customer through VehicleReader.
    customer_name VARCHAR(255) NOT NULL,
    vehicle_license_plate VARCHAR(20) NOT NULL,
    -- Rendered from the garage's quote_number_template (FR-TEN-05) when
    -- the quote is created.
    number VARCHAR(50) NOT NULL,
    status quote_status NOT NULL DEFAULT 'draft',
    approval_mode quote_approval_mode NOT NULL DEFAULT 'link',
    notes TEXT,
    -- Sum of quote_lines.line_total; denormalized for fast reads/listing.
    total_amount BIGINT NOT NULL DEFAULT 0,
    -- The garage's legal identity (FR-TEN-06) when the quote was created or
    -- sent: a later change of the profile never rewrites it.
    issuer JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Who the quote is billed to (FR-CUS-07), a snapshot of the customer's
    -- tax profile; empty is a retail receipt.
    billing_party JSONB NOT NULL DEFAULT '{}'::jsonb,
    public_token_hash CHAR(64),
    public_token_expires_at TIMESTAMP WITH TIME ZONE,
    sent_at TIMESTAMP WITH TIME ZONE,
    decided_at TIMESTAMP WITH TIME ZONE,
    converted_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    -- A quote's customer and vehicle must belong to the same garage.
    CONSTRAINT quotes_garage_customer_fkey FOREIGN KEY (garage_id, customer_id)
        REFERENCES customers (garage_id, id),
    CONSTRAINT quotes_garage_vehicle_fkey FOREIGN KEY (garage_id, vehicle_id)
        REFERENCES vehicles (garage_id, id),
    -- Target of the quote_lines (garage_id, quote_id) foreign key.
    CONSTRAINT quotes_garage_id_id_key UNIQUE (garage_id, id)
);

-- One live quote per number within a garage (Database Design Section 8).
CREATE UNIQUE INDEX idx_quotes_garage_number_unique ON quotes (garage_id, number) WHERE deleted_at IS NULL;

-- Status-filtered, time-sorted listing (Database Design Section 8).
CREATE INDEX idx_quotes_garage_status_created_at ON quotes (garage_id, status, created_at DESC) WHERE deleted_at IS NULL;

-- Filtering a garage's quotes by customer or vehicle, and vehicle history
-- (FR-CUS-03), newest first.
CREATE INDEX idx_quotes_customer_id ON quotes (garage_id, customer_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_quotes_vehicle_id ON quotes (garage_id, vehicle_id, created_at DESC) WHERE deleted_at IS NULL;
-- Which documents were billed to a tax profile (the delete-or-deactivate check).
CREATE INDEX idx_quotes_billing_profile ON quotes (garage_id, (billing_party->>'tax_profile_id'))
    WHERE deleted_at IS NULL AND billing_party->>'tax_profile_id' IS NOT NULL;

-- The public approval link looks a quote up by token alone (no garage_id
-- known yet); see find_quote_by_public_token() below.
CREATE UNIQUE INDEX idx_quotes_public_token_hash ON quotes (public_token_hash) WHERE public_token_hash IS NOT NULL;

ALTER TABLE quotes ENABLE ROW LEVEL SECURITY;
ALTER TABLE quotes FORCE ROW LEVEL SECURITY;

CREATE POLICY quotes_rls_policy ON quotes
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY quotes_rls_policy ON quotes IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- quote_lines: one labor or part line of a quote (garage_id is
-- denormalized from the parent quote so RLS and per-garage indexes apply
-- directly, the same pattern as every other tenant table).
-- ---------------------------------------------------------------------------
CREATE TABLE quote_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    quote_id UUID NOT NULL,
    -- Display order within the quote.
    position INTEGER NOT NULL DEFAULT 0,
    item_type quote_item_type NOT NULL,
    -- A parts-catalog reference once the Inventory module ships (Release
    -- 1.3); not yet a foreign key.
    item_ref_id UUID,
    description TEXT NOT NULL,
    -- NUMERIC(14,3): units and conversions (Release 6.0) need real decimals,
    -- and reports aggregate quantities in SQL. The API still sends and
    -- receives a decimal string ("1.5"); the query boundary casts.
    quantity NUMERIC(14, 3) NOT NULL DEFAULT 1,
    unit_price BIGINT NOT NULL DEFAULT 0,
    line_total BIGINT NOT NULL DEFAULT 0,
    -- The customer's decision (FR-QUO-03): pending until SendQuote/
    -- RecordQuoteDecision, then approved or rejected.
    decision quote_line_decision NOT NULL DEFAULT 'pending',
    decided_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID,
    CONSTRAINT quote_lines_garage_quote_fkey FOREIGN KEY (garage_id, quote_id)
        REFERENCES quotes (garage_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_quote_lines_quote_id ON quote_lines (garage_id, quote_id, position);

ALTER TABLE quote_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE quote_lines FORCE ROW LEVEL SECURITY;

CREATE POLICY quote_lines_rls_policy ON quote_lines
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY quote_lines_rls_policy ON quote_lines IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- find_quote_by_public_token: the customer approval link (FR-QUO-02) names
-- only an opaque token, not a garage, so the usual garage-scoped RLS
-- cannot apply yet -- the same problem owner_usage() (000013) solves for
-- cross-garage aggregates. This function runs SECURITY DEFINER and returns
-- only the two ids the caller needs to open a normally-scoped transaction
-- for that garage; it never returns quote data.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION find_quote_by_public_token(token_hash TEXT)
RETURNS TABLE (id UUID, garage_id UUID)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
    SELECT q.id, q.garage_id FROM quotes q
    WHERE q.public_token_hash = token_hash
      AND q.public_token_expires_at > now()
      AND q.deleted_at IS NULL
$$;

GRANT EXECUTE ON FUNCTION find_quote_by_public_token(TEXT) TO vietgara_app;
