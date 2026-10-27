-- supplier_returns and supplier_return_lines (FR-INV-10): stock leaves and the
-- supplier owes a credit. unit_cost is the credit per entered unit (VND);
-- average_cost is the stock's average-cost snapshot per base unit, used to
-- put the parts back when the return is cancelled.

CREATE TABLE public.supplier_returns (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    number character varying(50) NOT NULL,
    status public.adjustment_status DEFAULT 'completed'::public.adjustment_status NOT NULL,
    supplier_id uuid NOT NULL,
    goods_receipt_id uuid,
    credit_note_number character varying(100) DEFAULT ''::character varying NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    cancelled_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.supplier_returns FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.supplier_returns
    ADD CONSTRAINT supplier_returns_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.supplier_returns
    ADD CONSTRAINT supplier_returns_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_supplier_returns_garage_created_at ON public.supplier_returns USING btree (garage_id, created_at DESC);

CREATE UNIQUE INDEX idx_supplier_returns_garage_number_unique ON public.supplier_returns USING btree (garage_id, number);

CREATE INDEX idx_supplier_returns_supplier ON public.supplier_returns USING btree (garage_id, supplier_id);

ALTER TABLE ONLY public.supplier_returns
    ADD CONSTRAINT supplier_returns_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.supplier_returns
    ADD CONSTRAINT supplier_returns_garage_receipt_fkey FOREIGN KEY (garage_id, goods_receipt_id) REFERENCES public.goods_receipts(garage_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.supplier_returns
    ADD CONSTRAINT supplier_returns_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id) REFERENCES public.suppliers(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_supplier_returns_row_audit BEFORE INSERT OR UPDATE ON public.supplier_returns FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.supplier_returns ENABLE ROW LEVEL SECURITY;

CREATE POLICY supplier_returns_rls_policy ON public.supplier_returns USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY supplier_returns_rls_policy ON public.supplier_returns IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
