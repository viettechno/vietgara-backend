-- part_units: the packaging units of a part with their factor to the base
-- unit (FR-CAT-02): 1 case = 24 cans is factor 24 when the base unit is the
-- can. The base unit has no row. sale_price overrides base price * factor.

CREATE TABLE public.part_units (
    garage_id uuid NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid NOT NULL,
    factor numeric(18,6) NOT NULL,
    sale_price bigint,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT part_units_factor_check CHECK ((factor > (0)::numeric)),
    CONSTRAINT part_units_sale_price_check CHECK (((sale_price IS NULL) OR (sale_price >= 0)))
);

ALTER TABLE ONLY public.part_units FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.part_units
    ADD CONSTRAINT part_units_pkey PRIMARY KEY (garage_id, part_id, unit_id);

ALTER TABLE ONLY public.part_units
    ADD CONSTRAINT part_units_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.part_units
    ADD CONSTRAINT part_units_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_part_units_created_audit BEFORE INSERT ON public.part_units FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.part_units ENABLE ROW LEVEL SECURITY;

CREATE POLICY part_units_rls_policy ON public.part_units USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY part_units_rls_policy ON public.part_units IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
