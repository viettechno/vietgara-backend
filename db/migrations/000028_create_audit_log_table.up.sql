-- audit_log: append-only record of security-relevant and financial changes
-- (Security Architecture 7). The application role may only INSERT and
-- SELECT (see create_app_role).

CREATE TABLE public.audit_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    actor_user_id uuid,
    garage_id uuid,
    action character varying(100) NOT NULL,
    entity_type character varying(50) NOT NULL,
    entity_id character varying(100) NOT NULL,
    before jsonb,
    after jsonb,
    ip character varying(64),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

ALTER TABLE ONLY public.audit_log FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (id);

CREATE INDEX idx_audit_log_entity ON public.audit_log USING btree (entity_type, entity_id, created_at DESC);

CREATE INDEX idx_audit_log_garage ON public.audit_log USING btree (garage_id, created_at DESC) WHERE (garage_id IS NOT NULL);

CREATE TRIGGER trigger_audit_log_created_audit BEFORE INSERT ON public.audit_log FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.audit_log ENABLE ROW LEVEL SECURITY;

CREATE POLICY audit_log_insert ON public.audit_log FOR INSERT WITH CHECK (((garage_id IS NULL) OR (public.app_current_garage_id() IS NULL) OR (garage_id = public.app_current_garage_id())));

CREATE POLICY audit_log_select ON public.audit_log FOR SELECT USING (((garage_id IS NOT NULL) AND (garage_id = public.app_current_garage_id())));

-- Append-only for the application role.
REVOKE UPDATE, DELETE ON public.audit_log FROM vietgara_app;
