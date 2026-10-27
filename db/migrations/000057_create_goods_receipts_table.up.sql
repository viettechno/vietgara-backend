-- goods_receipts: a numbered multi-line delivery document (FR-INV-06). The
-- header supplier is a default that new lines inherit; every line carries its
-- own supplier. Completing posts the stock movements and updates the average
-- cost; cancelling a completed receipt reverses them.

CREATE TYPE public.goods_receipt_status AS ENUM (
    'draft',
    'completed',
    'cancelled'
);

CREATE TABLE public.goods_receipts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    number character varying(50) NOT NULL,
    status public.goods_receipt_status DEFAULT 'draft'::public.goods_receipt_status NOT NULL,
    default_supplier_id uuid,
    receipt_date date NOT NULL,
    delivery_note_number character varying(100) DEFAULT ''::character varying NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    completed_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.goods_receipts FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.goods_receipts
    ADD CONSTRAINT goods_receipts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.goods_receipts
    ADD CONSTRAINT goods_receipts_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_goods_receipts_garage_created_at ON public.goods_receipts USING btree (garage_id, created_at DESC);

CREATE UNIQUE INDEX idx_goods_receipts_garage_number_unique ON public.goods_receipts USING btree (garage_id, number);

ALTER TABLE ONLY public.goods_receipts
    ADD CONSTRAINT goods_receipts_garage_default_supplier_fkey FOREIGN KEY (garage_id, default_supplier_id) REFERENCES public.suppliers(garage_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.goods_receipts
    ADD CONSTRAINT goods_receipts_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_goods_receipts_row_audit BEFORE INSERT OR UPDATE ON public.goods_receipts FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.goods_receipts ENABLE ROW LEVEL SECURITY;

CREATE POLICY goods_receipts_rls_policy ON public.goods_receipts USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY goods_receipts_rls_policy ON public.goods_receipts IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
