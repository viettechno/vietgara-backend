CREATE TABLE public.supplier_return_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    supplier_return_id uuid NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid,
    entered_quantity numeric(14,3) NOT NULL,
    quantity numeric(14,3) NOT NULL,
    unit_cost bigint DEFAULT 0 NOT NULL,
    average_cost numeric(18,4),
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT supplier_return_lines_entered_quantity_check CHECK ((entered_quantity > (0)::numeric)),
    CONSTRAINT supplier_return_lines_quantity_check CHECK ((quantity > (0)::numeric)),
    CONSTRAINT supplier_return_lines_unit_cost_check CHECK ((unit_cost >= 0))
);

ALTER TABLE ONLY public.supplier_return_lines FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.supplier_return_lines
    ADD CONSTRAINT supplier_return_lines_pkey PRIMARY KEY (id);

CREATE INDEX idx_supplier_return_lines_part ON public.supplier_return_lines USING btree (garage_id, part_id);

CREATE INDEX idx_supplier_return_lines_return ON public.supplier_return_lines USING btree (garage_id, supplier_return_id, "position");

ALTER TABLE ONLY public.supplier_return_lines
    ADD CONSTRAINT supplier_return_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id);

ALTER TABLE ONLY public.supplier_return_lines
    ADD CONSTRAINT supplier_return_lines_garage_return_fkey FOREIGN KEY (garage_id, supplier_return_id) REFERENCES public.supplier_returns(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.supplier_return_lines
    ADD CONSTRAINT supplier_return_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_supplier_return_lines_row_audit BEFORE INSERT OR UPDATE ON public.supplier_return_lines FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.supplier_return_lines ENABLE ROW LEVEL SECURITY;

CREATE POLICY supplier_return_lines_rls_policy ON public.supplier_return_lines USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY supplier_return_lines_rls_policy ON public.supplier_return_lines IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
