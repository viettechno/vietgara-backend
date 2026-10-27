CREATE TYPE public.notification_channel AS ENUM (
    'email'
);

CREATE TYPE public.notification_status AS ENUM (
    'sent',
    'failed'
);

CREATE TABLE public.notification_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid,
    channel public.notification_channel NOT NULL,
    template character varying(100) NOT NULL,
    recipient character varying(255) NOT NULL,
    status public.notification_status NOT NULL,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

ALTER TABLE ONLY public.notification_log
    ADD CONSTRAINT notification_log_pkey PRIMARY KEY (id);

CREATE INDEX idx_notification_log_user ON public.notification_log USING btree (user_id, created_at DESC) WHERE (user_id IS NOT NULL);

CREATE TRIGGER trigger_notification_log_created_audit BEFORE INSERT ON public.notification_log FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

-- Append-only for the application role.
REVOKE UPDATE, DELETE ON public.notification_log FROM vietgara_app;
