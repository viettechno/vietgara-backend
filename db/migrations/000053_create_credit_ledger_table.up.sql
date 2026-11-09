-- The account credit ledger: append-only apart from remaining, which payments
-- and expiry lower. Entries that add credit are lots: they lapse at
-- expires_at (one year after they were earned) and payments use the oldest
-- first. subscriptions.credit_balance is the sum of the unexpired remaining.

CREATE TYPE public.credit_entry_kind AS ENUM (
    'earned',
    'applied',
    'adjustment',
    'expired'
);

CREATE TABLE public.credit_ledger (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    kind public.credit_entry_kind NOT NULL,
    amount bigint NOT NULL,
    balance_after bigint NOT NULL,
    expires_at timestamp with time zone,
    remaining bigint DEFAULT 0 NOT NULL,
    plan_payment_request_id uuid,
    note text,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT credit_ledger_balance_after_check CHECK ((balance_after >= 0)),
    CONSTRAINT credit_ledger_remaining_check CHECK ((remaining >= 0))
);

ALTER TABLE ONLY public.credit_ledger
    ADD CONSTRAINT credit_ledger_pkey PRIMARY KEY (id);

CREATE INDEX idx_credit_ledger_lots ON public.credit_ledger USING btree (user_id, expires_at) WHERE (remaining > 0);

CREATE INDEX idx_credit_ledger_user ON public.credit_ledger USING btree (user_id, created_at DESC);

ALTER TABLE ONLY public.credit_ledger
    ADD CONSTRAINT credit_ledger_plan_payment_request_id_fkey FOREIGN KEY (plan_payment_request_id) REFERENCES public.plan_payment_requests(id);

ALTER TABLE ONLY public.credit_ledger
    ADD CONSTRAINT credit_ledger_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_credit_ledger_created_audit BEFORE INSERT ON public.credit_ledger FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();
