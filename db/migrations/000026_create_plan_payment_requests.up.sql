-- Plan changes and renewals by manual payment (FR-LIC-02, FR-LIC-07..10).
-- An owner quotes a plan change or a renewal, transfers the money to the
-- platform's bank account and declares the transfer with its evidence; a
-- platform admin approves it and the plan applies at once. Account credit
-- carries the unused value of a period (and a downgrade's surplus) to the
-- next payment. Platform-level data without Row-Level Security; the License
-- module authorizes access.
CREATE TYPE plan_payment_kind AS ENUM ('change', 'renew');
CREATE TYPE plan_payment_status AS ENUM ('awaiting_payment', 'submitted', 'approved', 'rejected', 'cancelled');
CREATE TYPE credit_entry_kind AS ENUM ('earned', 'applied', 'adjustment');

CREATE TABLE plan_payment_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subscription_id UUID NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    kind plan_payment_kind NOT NULL,
    plan_id UUID NOT NULL REFERENCES plans(id),
    status plan_payment_status NOT NULL DEFAULT 'awaiting_payment',
    -- The code the owner puts in the transfer note, e.g. VG-7K2M9Q.
    reference VARCHAR(20) NOT NULL UNIQUE,
    -- The quote, computed when the request starts and honoured at approval.
    quoted_amount BIGINT NOT NULL CHECK (quoted_amount >= 0),
    credit_applied BIGINT NOT NULL CHECK (credit_applied >= 0),
    amount_due BIGINT NOT NULL CHECK (amount_due >= 0),
    credit_surplus BIGINT NOT NULL CHECK (credit_surplus >= 0),
    -- The part of the available credit that is the unused value of the
    -- current period (earned when the plan changes); the rest is the balance.
    unused_credit BIGINT NOT NULL DEFAULT 0 CHECK (unused_credit >= 0),
    period_days INTEGER NOT NULL,
    unused_days INTEGER NOT NULL,
    -- The platform's payment details shown when the request was quoted.
    instructions JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- The owner's declaration, set when submitted.
    payer_name VARCHAR(255),
    payer_bank VARCHAR(255),
    payer_account VARCHAR(100),
    transferred_amount BIGINT CHECK (transferred_amount IS NULL OR transferred_amount >= 0),
    bank_transaction_ref VARCHAR(100),
    paid_at TIMESTAMP WITH TIME ZONE,
    evidence_file_id UUID REFERENCES files(id),
    amount_mismatch BOOLEAN NOT NULL DEFAULT FALSE,
    review_note TEXT,
    rejection_reason TEXT,
    reviewed_by UUID,
    reviewed_at TIMESTAMP WITH TIME ZONE,
    submitted_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- One open request per owner.
CREATE UNIQUE INDEX idx_plan_payment_requests_open ON plan_payment_requests (user_id)
    WHERE status IN ('awaiting_payment', 'submitted');
-- The review queue, oldest first.
CREATE INDEX idx_plan_payment_requests_review ON plan_payment_requests (submitted_at) WHERE status = 'submitted';
CREATE INDEX idx_plan_payment_requests_user ON plan_payment_requests (user_id, created_at DESC);

CREATE TRIGGER trigger_plan_payment_requests_row_audit BEFORE INSERT OR UPDATE ON plan_payment_requests
    FOR EACH ROW EXECUTE FUNCTION set_row_audit();

-- The account credit ledger: append-only; subscriptions.credit_balance is
-- the running total.
CREATE TABLE credit_ledger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind credit_entry_kind NOT NULL,
    amount BIGINT NOT NULL,
    balance_after BIGINT NOT NULL CHECK (balance_after >= 0),
    plan_payment_request_id UUID REFERENCES plan_payment_requests(id),
    note TEXT,
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_credit_ledger_user ON credit_ledger (user_id, created_at DESC);

CREATE TRIGGER trigger_credit_ledger_created_audit BEFORE INSERT ON credit_ledger
    FOR EACH ROW EXECUTE FUNCTION set_created_audit();

-- The platform's payment details: one row.
CREATE TABLE platform_payment_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    bank_name VARCHAR(255) NOT NULL DEFAULT '',
    account_number VARCHAR(100) NOT NULL DEFAULT '',
    account_holder VARCHAR(255) NOT NULL DEFAULT '',
    branch VARCHAR(255) NOT NULL DEFAULT '',
    instructions TEXT NOT NULL DEFAULT '',
    -- A files row of kind platform_qr.
    qr_file_id UUID REFERENCES files(id),
    -- When the admins were last reminded of requests waiting for review.
    last_review_reminder_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

INSERT INTO platform_payment_settings (id) VALUES (1);

CREATE TRIGGER trigger_platform_payment_settings_row_audit BEFORE INSERT OR UPDATE ON platform_payment_settings
    FOR EACH ROW EXECUTE FUNCTION set_row_audit();
