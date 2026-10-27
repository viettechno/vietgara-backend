-- part_suppliers: the usual suppliers of a part (FR-INV-19) with the
-- supplier's own code and the last cost paid per base unit; at most one is
-- the default that pre-fills a receipt line.

CREATE TABLE public.part_suppliers (
    garage_id uuid NOT NULL,
    part_id uuid NOT NULL,
    supplier_id uuid NOT NULL,
    supplier_sku character varying(100) DEFAULT ''::character varying NOT NULL,
    last_unit_cost numeric(18,4),
    is_default boolean DEFAULT false NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.part_suppliers FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.part_suppliers
    ADD CONSTRAINT part_suppliers_pkey PRIMARY KEY (garage_id, part_id, supplier_id);

CREATE UNIQUE INDEX idx_part_suppliers_default_unique ON public.part_suppliers USING btree (garage_id, part_id) WHERE is_default;

ALTER TABLE ONLY public.part_suppliers
    ADD CONSTRAINT part_suppliers_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.part_suppliers
    ADD CONSTRAINT part_suppliers_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id) REFERENCES public.suppliers(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_part_suppliers_row_audit BEFORE INSERT OR UPDATE ON public.part_suppliers FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.part_suppliers ENABLE ROW LEVEL SECURITY;

CREATE POLICY part_suppliers_rls_policy ON public.part_suppliers USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY part_suppliers_rls_policy ON public.part_suppliers IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
