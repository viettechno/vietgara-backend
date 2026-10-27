-- settlement_payments: record of money actually collected, never deleted
-- (FR-BIL-03); a mistaken one is voided. A settlement may receive several until the total is fully
-- collected.

CREATE TYPE public.payment_method AS ENUM (
    'cash',
    'bank_transfer',
    'online'
);

CREATE TABLE public.settlement_payments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    settlement_id uuid NOT NULL,
    amount bigint NOT NULL,
    method public.payment_method NOT NULL,
    note text,
    voucher_number character varying(50) NOT NULL,
    payer_name character varying(255) NOT NULL,
    payer_address text,
    reason text NOT NULL,
    collector_name character varying(255) NOT NULL,
    issuer jsonb DEFAULT '{}'::jsonb NOT NULL,
    void_reason text,
    voided_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT settlement_payments_void_check CHECK (((voided_at IS NULL) = (void_reason IS NULL)))
);

ALTER TABLE ONLY public.settlement_payments FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.settlement_payments
    ADD CONSTRAINT settlement_payments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.settlement_payments
    ADD CONSTRAINT settlement_payments_garage_voucher_unique UNIQUE (garage_id, voucher_number);

CREATE INDEX idx_settlement_payments_settlement_id ON public.settlement_payments USING btree (garage_id, settlement_id, created_at);

ALTER TABLE ONLY public.settlement_payments
    ADD CONSTRAINT settlement_payments_garage_settlement_fkey FOREIGN KEY (garage_id, settlement_id) REFERENCES public.settlements(garage_id, id) ON DELETE CASCADE;

CREATE TRIGGER trigger_settlement_payments_row_audit BEFORE INSERT OR UPDATE ON public.settlement_payments FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.settlement_payments ENABLE ROW LEVEL SECURITY;

CREATE POLICY settlement_payments_rls_policy ON public.settlement_payments USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY settlement_payments_rls_policy ON public.settlement_payments IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
