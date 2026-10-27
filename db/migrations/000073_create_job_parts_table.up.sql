-- job_parts: the parts a job suggests. quantity is the amount typed in
-- unit_id (the base unit when NULL). A suggested part is listed once per
-- job. The unit key is composite so the unit is a packaging unit of the
-- same part; removing such a packaging unit is refused while a job uses it.

CREATE TABLE public.job_parts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    job_id uuid NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid,
    quantity numeric(14,3) NOT NULL,
    non_billable boolean DEFAULT false NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT job_parts_quantity_check CHECK ((quantity > (0)::numeric))
);

ALTER TABLE ONLY public.job_parts FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.job_parts
    ADD CONSTRAINT job_parts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.job_parts
    ADD CONSTRAINT job_parts_job_part_key UNIQUE (job_id, part_id);

CREATE INDEX idx_job_parts_part ON public.job_parts USING btree (garage_id, part_id);

ALTER TABLE ONLY public.job_parts
    ADD CONSTRAINT job_parts_garage_job_fkey FOREIGN KEY (garage_id, job_id) REFERENCES public.jobs(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.job_parts
    ADD CONSTRAINT job_parts_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.job_parts
    ADD CONSTRAINT job_parts_garage_part_unit_fkey FOREIGN KEY (garage_id, part_id, unit_id) REFERENCES public.part_units(garage_id, part_id, unit_id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_job_parts_row_audit BEFORE INSERT OR UPDATE ON public.job_parts FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.job_parts ENABLE ROW LEVEL SECURITY;

CREATE POLICY job_parts_rls_policy ON public.job_parts USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY job_parts_rls_policy ON public.job_parts IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
