CREATE TABLE public.support_access_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    grant_id uuid NOT NULL,
    garage_id uuid NOT NULL,
    staff_user_id uuid NOT NULL,
    method character varying(100) NOT NULL,
    entity_type character varying(50) NOT NULL,
    entity_id character varying(100) DEFAULT ''::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

ALTER TABLE ONLY public.support_access_events FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.support_access_events
    ADD CONSTRAINT support_access_events_pkey PRIMARY KEY (id);

CREATE INDEX idx_support_access_events_garage ON public.support_access_events USING btree (garage_id, created_at DESC);

CREATE INDEX idx_support_access_events_grant ON public.support_access_events USING btree (grant_id, created_at DESC);

ALTER TABLE ONLY public.support_access_events
    ADD CONSTRAINT support_access_events_grant_id_fkey FOREIGN KEY (grant_id) REFERENCES public.support_access_grants(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_support_access_events_created_audit BEFORE INSERT ON public.support_access_events FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.support_access_events ENABLE ROW LEVEL SECURITY;

CREATE POLICY support_access_events_insert ON public.support_access_events FOR INSERT WITH CHECK ((staff_user_id = public.app_current_user_id()));

CREATE POLICY support_access_events_select ON public.support_access_events FOR SELECT USING ((garage_id = public.app_current_garage_id()));

-- Append-only for the application role.
REVOKE UPDATE, DELETE ON public.support_access_events FROM vietgara_app;
