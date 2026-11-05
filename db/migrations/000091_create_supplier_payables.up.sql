-- Supplier payables and payment-out vouchers (Release 13.0, FR-INV-17,
-- FR-BIL-07): what the garage owes each supplier for a completed goods
-- receipt, and the vouchers (phiếu chi) that settle it. A voucher is never
-- deleted, only voided.

CREATE TYPE public.payable_status AS ENUM (
    'open',
    'paid',
    'cancelled'
);

CREATE TABLE public.supplier_payables (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    goods_receipt_id uuid NOT NULL,
    goods_receipt_number character varying(50) NOT NULL,
    supplier_id uuid NOT NULL,
    supplier_name character varying(255) NOT NULL,
    receipt_date date NOT NULL,
    total_amount bigint NOT NULL,
    paid_amount bigint DEFAULT 0 NOT NULL,
    status public.payable_status DEFAULT 'open'::public.payable_status NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT supplier_payables_amounts_check CHECK (((total_amount > 0) AND (paid_amount >= 0) AND (paid_amount <= total_amount)))
);

ALTER TABLE ONLY public.supplier_payables FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.supplier_payables
    ADD CONSTRAINT supplier_payables_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.supplier_payables
    ADD CONSTRAINT supplier_payables_garage_id_id_key UNIQUE (garage_id, id);

ALTER TABLE ONLY public.supplier_payables
    ADD CONSTRAINT supplier_payables_receipt_supplier_unique UNIQUE (garage_id, goods_receipt_id, supplier_id);

CREATE INDEX idx_supplier_payables_supplier ON public.supplier_payables USING btree (garage_id, supplier_id, status);

ALTER TABLE ONLY public.supplier_payables
    ADD CONSTRAINT supplier_payables_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.supplier_payables
    ADD CONSTRAINT supplier_payables_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id) REFERENCES public.suppliers(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_supplier_payables_row_audit BEFORE INSERT OR UPDATE ON public.supplier_payables FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.supplier_payables ENABLE ROW LEVEL SECURITY;

CREATE POLICY supplier_payables_rls_policy ON public.supplier_payables USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY supplier_payables_rls_policy ON public.supplier_payables IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

CREATE TABLE public.payment_out_vouchers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    number character varying(50) NOT NULL,
    supplier_id uuid NOT NULL,
    supplier_name character varying(255) NOT NULL,
    payee_name character varying(255) NOT NULL,
    payee_address text,
    amount bigint NOT NULL,
    method public.payment_method NOT NULL,
    reason text NOT NULL,
    note text,
    issuer jsonb DEFAULT '{}'::jsonb NOT NULL,
    issuer_name character varying(255) NOT NULL,
    void_reason text,
    voided_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT payment_out_vouchers_amount_check CHECK ((amount > 0)),
    CONSTRAINT payment_out_vouchers_void_check CHECK (((voided_at IS NULL) = (void_reason IS NULL)))
);

ALTER TABLE ONLY public.payment_out_vouchers FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.payment_out_vouchers
    ADD CONSTRAINT payment_out_vouchers_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.payment_out_vouchers
    ADD CONSTRAINT payment_out_vouchers_garage_id_id_key UNIQUE (garage_id, id);

ALTER TABLE ONLY public.payment_out_vouchers
    ADD CONSTRAINT payment_out_vouchers_garage_number_unique UNIQUE (garage_id, number);

CREATE INDEX idx_payment_out_vouchers_supplier ON public.payment_out_vouchers USING btree (garage_id, supplier_id, created_at DESC);

ALTER TABLE ONLY public.payment_out_vouchers
    ADD CONSTRAINT payment_out_vouchers_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.payment_out_vouchers
    ADD CONSTRAINT payment_out_vouchers_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id) REFERENCES public.suppliers(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_payment_out_vouchers_row_audit BEFORE INSERT OR UPDATE ON public.payment_out_vouchers FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.payment_out_vouchers ENABLE ROW LEVEL SECURITY;

CREATE POLICY payment_out_vouchers_rls_policy ON public.payment_out_vouchers USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY payment_out_vouchers_rls_policy ON public.payment_out_vouchers IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

CREATE TABLE public.payment_out_allocations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    voucher_id uuid NOT NULL,
    payable_id uuid NOT NULL,
    amount bigint NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT payment_out_allocations_amount_check CHECK ((amount > 0))
);

ALTER TABLE ONLY public.payment_out_allocations FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.payment_out_allocations
    ADD CONSTRAINT payment_out_allocations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.payment_out_allocations
    ADD CONSTRAINT payment_out_allocations_voucher_payable_unique UNIQUE (voucher_id, payable_id);

CREATE INDEX idx_payment_out_allocations_payable ON public.payment_out_allocations USING btree (garage_id, payable_id);

ALTER TABLE ONLY public.payment_out_allocations
    ADD CONSTRAINT payment_out_allocations_garage_voucher_fkey FOREIGN KEY (garage_id, voucher_id) REFERENCES public.payment_out_vouchers(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.payment_out_allocations
    ADD CONSTRAINT payment_out_allocations_garage_payable_fkey FOREIGN KEY (garage_id, payable_id) REFERENCES public.supplier_payables(garage_id, id) ON DELETE CASCADE;

CREATE TRIGGER trigger_payment_out_allocations_created_audit BEFORE INSERT ON public.payment_out_allocations FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.payment_out_allocations ENABLE ROW LEVEL SECURITY;

CREATE POLICY payment_out_allocations_rls_policy ON public.payment_out_allocations USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY payment_out_allocations_rls_policy ON public.payment_out_allocations IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
