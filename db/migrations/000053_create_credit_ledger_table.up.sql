-- The account credit ledger: append-only; subscriptions.credit_balance is
-- the running total.

CREATE TYPE public.credit_entry_kind AS ENUM (
    'earned',
    'applied',
    'adjustment'
);

CREATE TABLE public.credit_ledger (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    kind public.credit_entry_kind NOT NULL,
    amount bigint NOT NULL,
    balance_after bigint NOT NULL,
    plan_payment_request_id uuid,
    note text,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT credit_ledger_balance_after_check CHECK ((balance_after >= 0))
);

ALTER TABLE ONLY public.credit_ledger
    ADD CONSTRAINT credit_ledger_pkey PRIMARY KEY (id);

CREATE INDEX idx_credit_ledger_user ON public.credit_ledger USING btree (user_id, created_at DESC);

ALTER TABLE ONLY public.credit_ledger
    ADD CONSTRAINT credit_ledger_plan_payment_request_id_fkey FOREIGN KEY (plan_payment_request_id) REFERENCES public.plan_payment_requests(id);

ALTER TABLE ONLY public.credit_ledger
    ADD CONSTRAINT credit_ledger_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_credit_ledger_created_audit BEFORE INSERT ON public.credit_ledger FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();
