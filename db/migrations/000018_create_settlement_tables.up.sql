-- Billing & Settlement module (HLD module #8), Database Design Section 6.1.
-- The final bill of a closed repair order (FR-BIL-01), paid by the
-- customer, with payments recorded as they are actually collected
-- (FR-BIL-03). No separate receivables table: FR-REC-01's "list of
-- settlements not yet fully collected" is this table filtered by status --
-- Release 2.2's reminders and reconciliation are what will need their own
-- persisted state, not the list itself.

CREATE TYPE settlement_status AS ENUM ('pending', 'partially_paid', 'paid');
-- 'online' is recorded manually in Release 1.4 (staff sees a bank app or
-- gateway dashboard confirmation and records it); automatic confirmation
-- via a payment gateway webhook is Release 2.0 (FR-PAY-02).
CREATE TYPE payment_method AS ENUM ('cash', 'bank_transfer', 'online');

CREATE TABLE settlements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    repair_order_id UUID NOT NULL,
    -- Denormalized from the repair order at creation, the same pattern as
    -- repair_orders.customer_name/vehicle_license_plate (HLD Section 2:
    -- each module owns its own tables and never joins another module's).
    customer_name VARCHAR(255) NOT NULL,
    vehicle_license_plate VARCHAR(20) NOT NULL,
    -- Rendered from the garage's settlement_number_template (FR-TEN-05).
    number VARCHAR(50) NOT NULL,
    -- Copied from the repair order's total at creation time.
    total_amount BIGINT NOT NULL DEFAULT 0,
    -- Running total from settlement_payments (FR-BIL-03).
    paid_amount BIGINT NOT NULL DEFAULT 0,
    -- The garage's legal identity (FR-TEN-06) when the settlement was
    -- created: the receipt prints it.
    issuer JSONB NOT NULL DEFAULT '{}'::jsonb,
    status settlement_status NOT NULL DEFAULT 'pending',
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT settlements_garage_ro_fkey FOREIGN KEY (garage_id, repair_order_id)
        REFERENCES repair_orders (garage_id, id),
    -- Target of the settlement_payments (garage_id, settlement_id) foreign key.
    CONSTRAINT settlements_garage_id_id_key UNIQUE (garage_id, id),
    -- A repair order settles at most once.
    CONSTRAINT settlements_repair_order_id_unique UNIQUE (repair_order_id),
    CONSTRAINT settlements_garage_number_unique UNIQUE (garage_id, number)
);

CREATE INDEX idx_settlements_garage_status_created_at ON settlements (garage_id, status, created_at DESC);

ALTER TABLE settlements ENABLE ROW LEVEL SECURITY;
ALTER TABLE settlements FORCE ROW LEVEL SECURITY;

CREATE POLICY settlements_rls_policy ON settlements
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY settlements_rls_policy ON settlements IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- settlement_payments: append-only record of money actually collected
-- (FR-BIL-03). A settlement may receive several until the total is fully
-- collected.
-- ---------------------------------------------------------------------------
CREATE TABLE settlement_payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    settlement_id UUID NOT NULL,
    amount BIGINT NOT NULL,
    method payment_method NOT NULL,
    note TEXT,
    -- Every payment is a payment voucher (phiếu thu, FR-BIL-05): numbered
    -- from the garage's voucher template, with who paid, why, and who
    -- collected. issuer is the garage's legal identity (FR-TEN-06) when the
    -- voucher was issued, so a later change of the profile never rewrites
    -- it.
    voucher_number VARCHAR(50) NOT NULL,
    payer_name VARCHAR(255) NOT NULL,
    payer_address TEXT,
    reason TEXT NOT NULL,
    collector_name VARCHAR(255) NOT NULL,
    issuer JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT settlement_payments_garage_settlement_fkey FOREIGN KEY (garage_id, settlement_id)
        REFERENCES settlements (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT settlement_payments_garage_voucher_unique UNIQUE (garage_id, voucher_number)
);

CREATE INDEX idx_settlement_payments_settlement_id ON settlement_payments (garage_id, settlement_id, created_at);

ALTER TABLE settlement_payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE settlement_payments FORCE ROW LEVEL SECURITY;

CREATE POLICY settlement_payments_rls_policy ON settlement_payments
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY settlement_payments_rls_policy ON settlement_payments IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
