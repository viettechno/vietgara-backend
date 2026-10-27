-- import_runs: one row per confirmed Excel import (FR-INV-14). Its unique
-- (garage, type, file hash) key is the idempotency guard: confirming the
-- same content twice cannot create a second document. It doubles as the
-- import history and keeps the document the import created.

CREATE TABLE public.import_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    import_type character varying(32) NOT NULL,
    file_sha256 character(64) NOT NULL,
    file_name character varying(255) DEFAULT ''::character varying NOT NULL,
    row_count integer DEFAULT 0 NOT NULL,
    document_id uuid,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT import_runs_import_type_check CHECK (((import_type)::text = ANY ((ARRAY['parts'::character varying, 'goods_receipts'::character varying, 'stock_counts'::character varying])::text[]))),
    CONSTRAINT import_runs_row_count_check CHECK ((row_count >= 0))
);

ALTER TABLE ONLY public.import_runs FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.import_runs
    ADD CONSTRAINT import_runs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.import_runs
    ADD CONSTRAINT import_runs_garage_id_id_key UNIQUE (garage_id, id);

ALTER TABLE ONLY public.import_runs
    ADD CONSTRAINT import_runs_garage_type_file_key UNIQUE (garage_id, import_type, file_sha256);

CREATE INDEX idx_import_runs_garage_created_at ON public.import_runs USING btree (garage_id, created_at DESC);

ALTER TABLE ONLY public.import_runs
    ADD CONSTRAINT import_runs_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_import_runs_created_audit BEFORE INSERT ON public.import_runs FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.import_runs ENABLE ROW LEVEL SECURITY;

CREATE POLICY import_runs_rls_policy ON public.import_runs USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY import_runs_rls_policy ON public.import_runs IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
