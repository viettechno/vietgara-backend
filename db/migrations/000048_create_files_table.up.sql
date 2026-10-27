CREATE TYPE public.file_kind AS ENUM (
    'logo',
    'payment_evidence',
    'platform_qr',
    'part_image'
);

CREATE TABLE public.files (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid,
    user_id uuid,
    kind public.file_kind NOT NULL,
    entity_type character varying(50) NOT NULL,
    entity_id uuid NOT NULL,
    key character varying(500) NOT NULL,
    content_type character varying(100) NOT NULL,
    size bigint NOT NULL,
    sha256 character(64) NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT files_one_owner CHECK (((garage_id IS NOT NULL) <> (user_id IS NOT NULL))),
    CONSTRAINT files_size_check CHECK ((size >= 0))
);

ALTER TABLE ONLY public.files FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_key_key UNIQUE (key);

CREATE INDEX idx_files_garage_entity ON public.files USING btree (garage_id, entity_type, entity_id) WHERE (deleted_at IS NULL);

CREATE INDEX idx_files_user ON public.files USING btree (user_id, kind) WHERE ((deleted_at IS NULL) AND (user_id IS NOT NULL));

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_files_row_audit BEFORE INSERT OR UPDATE ON public.files FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.files ENABLE ROW LEVEL SECURITY;

CREATE POLICY files_rls_policy ON public.files USING ((((garage_id IS NOT NULL) AND (garage_id = public.app_current_garage_id())) OR ((user_id IS NOT NULL) AND (user_id = public.app_current_user_id())))) WITH CHECK ((((garage_id IS NOT NULL) AND (garage_id = public.app_current_garage_id())) OR ((user_id IS NOT NULL) AND (user_id = public.app_current_user_id()))));

COMMENT ON POLICY files_rls_policy ON public.files IS 'A garage file is visible in its garage (app.current_garage_id); a user file to its user (app.current_user_id).';
