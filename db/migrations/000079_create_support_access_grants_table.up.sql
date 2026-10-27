CREATE TABLE public.support_access_grants (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    staff_user_id uuid NOT NULL,
    reason text NOT NULL,
    scope character varying(20) DEFAULT 'read_only'::character varying NOT NULL,
    ttl_seconds integer NOT NULL,
    requested_at timestamp with time zone DEFAULT now() NOT NULL,
    approved_by uuid,
    approved_at timestamp with time zone,
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone,
    granted_by_platform_admin boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid,
    CONSTRAINT support_access_grants_approval CHECK (((approved_at IS NULL) = (expires_at IS NULL))),
    CONSTRAINT support_access_grants_scope_check CHECK (((scope)::text = 'read_only'::text)),
    CONSTRAINT support_access_grants_ttl_seconds_check CHECK (((ttl_seconds >= 3600) AND (ttl_seconds <= 604800)))
);

ALTER TABLE ONLY public.support_access_grants FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.support_access_grants
    ADD CONSTRAINT support_access_grants_pkey PRIMARY KEY (id);

CREATE INDEX idx_support_access_grants_garage ON public.support_access_grants USING btree (garage_id, requested_at DESC);

CREATE INDEX idx_support_access_grants_open ON public.support_access_grants USING btree (garage_id, staff_user_id) WHERE (revoked_at IS NULL);

CREATE INDEX idx_support_access_grants_staff ON public.support_access_grants USING btree (staff_user_id, requested_at DESC);

ALTER TABLE ONLY public.support_access_grants
    ADD CONSTRAINT support_access_grants_approved_by_fkey FOREIGN KEY (approved_by) REFERENCES public.users(id);

ALTER TABLE ONLY public.support_access_grants
    ADD CONSTRAINT support_access_grants_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.support_access_grants
    ADD CONSTRAINT support_access_grants_staff_user_id_fkey FOREIGN KEY (staff_user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_support_access_grants_row_audit BEFORE INSERT OR UPDATE ON public.support_access_grants FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.support_access_grants ENABLE ROW LEVEL SECURITY;

CREATE POLICY support_access_grants_insert ON public.support_access_grants FOR INSERT WITH CHECK ((staff_user_id = public.app_current_user_id()));

CREATE POLICY support_access_grants_select ON public.support_access_grants FOR SELECT USING (((garage_id = public.app_current_garage_id()) OR (staff_user_id = public.app_current_user_id())));

CREATE POLICY support_access_grants_update ON public.support_access_grants FOR UPDATE USING (((garage_id = public.app_current_garage_id()) OR (staff_user_id = public.app_current_user_id()))) WITH CHECK (((garage_id = public.app_current_garage_id()) OR (staff_user_id = public.app_current_user_id())));
