-- jobs: a type of work with a standard labor charge (FR-CAT-05).

CREATE TABLE public.jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    category character varying(100) DEFAULT ''::character varying NOT NULL,
    labor_price bigint DEFAULT 0 NOT NULL,
    duration_minutes integer DEFAULT 0 NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT jobs_duration_minutes_check CHECK ((duration_minutes >= 0)),
    CONSTRAINT jobs_labor_price_check CHECK ((labor_price >= 0))
);

ALTER TABLE ONLY public.jobs FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.jobs
    ADD CONSTRAINT jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.jobs
    ADD CONSTRAINT jobs_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_jobs_garage_category ON public.jobs USING btree (garage_id, lower((category)::text)) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX idx_jobs_garage_name_unique ON public.jobs USING btree (garage_id, lower((name)::text)) WHERE (deleted_at IS NULL);

ALTER TABLE ONLY public.jobs
    ADD CONSTRAINT jobs_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_jobs_row_audit BEFORE INSERT OR UPDATE ON public.jobs FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.jobs ENABLE ROW LEVEL SECURITY;

CREATE POLICY jobs_rls_policy ON public.jobs USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY jobs_rls_policy ON public.jobs IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
