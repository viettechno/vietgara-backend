-- units: the garage's units of measure (FR-CAT-01). A unit in use is
-- deactivated (is_active = false), never hard-deleted (FR-CAT-07).

CREATE TABLE public.units (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    name character varying(50) NOT NULL,
    abbreviation character varying(20) NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);

ALTER TABLE ONLY public.units FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.units
    ADD CONSTRAINT units_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.units
    ADD CONSTRAINT units_garage_id_id_key UNIQUE (garage_id, id);

CREATE UNIQUE INDEX idx_units_garage_name_unique ON public.units USING btree (garage_id, lower((name)::text)) WHERE (deleted_at IS NULL);

ALTER TABLE ONLY public.units
    ADD CONSTRAINT units_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_units_row_audit BEFORE INSERT OR UPDATE ON public.units FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.units ENABLE ROW LEVEL SECURITY;

CREATE POLICY units_rls_policy ON public.units USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY units_rls_policy ON public.units IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
