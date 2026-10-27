-- goods_receipt_lines: one part of a receipt with its supplier. entered_*
-- is what the user typed (in unit_id, NULL for the base unit); quantity is
-- the derived base-unit amount. unit_cost is VND per entered unit as typed;
-- 0 means "not recorded".

CREATE TABLE public.goods_receipt_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    goods_receipt_id uuid NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid,
    entered_quantity numeric(14,3) NOT NULL,
    quantity numeric(14,3) NOT NULL,
    supplier_id uuid,
    unit_cost bigint DEFAULT 0 NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT goods_receipt_lines_entered_quantity_check CHECK ((entered_quantity > (0)::numeric)),
    CONSTRAINT goods_receipt_lines_quantity_check CHECK ((quantity > (0)::numeric)),
    CONSTRAINT goods_receipt_lines_unit_cost_check CHECK ((unit_cost >= 0))
);

ALTER TABLE ONLY public.goods_receipt_lines FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.goods_receipt_lines
    ADD CONSTRAINT goods_receipt_lines_pkey PRIMARY KEY (id);

CREATE INDEX idx_goods_receipt_lines_part ON public.goods_receipt_lines USING btree (garage_id, part_id);

CREATE INDEX idx_goods_receipt_lines_receipt ON public.goods_receipt_lines USING btree (garage_id, goods_receipt_id, "position");

CREATE INDEX idx_goods_receipt_lines_supplier ON public.goods_receipt_lines USING btree (garage_id, supplier_id);

ALTER TABLE ONLY public.goods_receipt_lines
    ADD CONSTRAINT goods_receipt_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id);

ALTER TABLE ONLY public.goods_receipt_lines
    ADD CONSTRAINT goods_receipt_lines_garage_receipt_fkey FOREIGN KEY (garage_id, goods_receipt_id) REFERENCES public.goods_receipts(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.goods_receipt_lines
    ADD CONSTRAINT goods_receipt_lines_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id) REFERENCES public.suppliers(garage_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.goods_receipt_lines
    ADD CONSTRAINT goods_receipt_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_goods_receipt_lines_row_audit BEFORE INSERT OR UPDATE ON public.goods_receipt_lines FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.goods_receipt_lines ENABLE ROW LEVEL SECURITY;

CREATE POLICY goods_receipt_lines_rls_policy ON public.goods_receipt_lines USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY goods_receipt_lines_rls_policy ON public.goods_receipt_lines IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
