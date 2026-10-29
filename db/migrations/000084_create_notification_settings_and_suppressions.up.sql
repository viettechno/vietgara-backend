-- Notification (Release 10.1, FR-NOT-05): the garage's e-mail switches and
-- sender name, the addresses that bounced, and a delivery log that knows its
-- garage, customer and the entity an e-mail is about.

CREATE TABLE public.garage_notification_settings (
    garage_id uuid NOT NULL,
    sender_name character varying(100),
    types jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid
);

ALTER TABLE ONLY public.garage_notification_settings FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.garage_notification_settings
    ADD CONSTRAINT garage_notification_settings_pkey PRIMARY KEY (garage_id);

ALTER TABLE ONLY public.garage_notification_settings
    ADD CONSTRAINT garage_notification_settings_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_garage_notification_settings_row_audit BEFORE INSERT OR UPDATE ON public.garage_notification_settings FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.garage_notification_settings ENABLE ROW LEVEL SECURITY;

CREATE POLICY garage_notification_settings_rls_policy ON public.garage_notification_settings USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY garage_notification_settings_rls_policy ON public.garage_notification_settings IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- Addresses that bounced for good or complained, across garages.
CREATE TABLE public.email_suppressions (
    email character varying(255) NOT NULL,
    reason character varying(20) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    CONSTRAINT email_suppressions_reason_check CHECK (((reason)::text = ANY ((ARRAY['bounce'::character varying, 'blocked'::character varying, 'spam'::character varying])::text[])))
);

ALTER TABLE ONLY public.email_suppressions
    ADD CONSTRAINT email_suppressions_pkey PRIMARY KEY (email);

CREATE TRIGGER trigger_email_suppressions_created_audit BEFORE INSERT ON public.email_suppressions FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

-- notification_log keeps the account and licence e-mails (no garage) and
-- gains the garage e-mails.
ALTER TYPE public.notification_status ADD VALUE IF NOT EXISTS 'skipped';

ALTER TABLE public.notification_log
    ADD COLUMN garage_id uuid,
    ADD COLUMN customer_id uuid,
    ADD COLUMN type character varying(30),
    ADD COLUMN entity_type character varying(30),
    ADD COLUMN entity_id uuid,
    ADD COLUMN event_id uuid,
    ADD COLUMN skip_reason character varying(20),
    ADD CONSTRAINT notification_log_skip_reason_check CHECK (((skip_reason)::text = ANY ((ARRAY['disabled'::character varying, 'opted_out'::character varying, 'suppressed'::character varying, 'no_email'::character varying])::text[])));

ALTER TABLE ONLY public.notification_log
    ADD CONSTRAINT notification_log_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE INDEX idx_notification_log_garage ON public.notification_log USING btree (garage_id, created_at DESC) WHERE (garage_id IS NOT NULL);

ALTER TABLE ONLY public.notification_log FORCE ROW LEVEL SECURITY;

ALTER TABLE public.notification_log ENABLE ROW LEVEL SECURITY;

-- A garage reads its own rows. The account and licence e-mails have no garage;
-- they are read, in a garage's scope, for the garage's owner and active staff
-- only (support diagnostics, Release 8.2), and by no other scope. Anyone may
-- write either kind.
CREATE POLICY notification_log_rls_policy ON public.notification_log
    USING (
        ((garage_id IS NOT NULL) AND (garage_id = public.app_current_garage_id()))
        OR ((garage_id IS NULL) AND (public.app_current_garage_id() IS NOT NULL) AND (user_id IN (
            SELECT g.user_id FROM public.garages g WHERE g.id = public.app_current_garage_id()
            UNION
            SELECT s.user_id FROM public.garage_staff s WHERE s.garage_id = public.app_current_garage_id() AND s.status = 'active'
        )))
    )
    WITH CHECK (((garage_id IS NULL) OR (garage_id = public.app_current_garage_id())));

COMMENT ON POLICY notification_log_rls_policy ON public.notification_log IS 'A garage reads its own sends, and the account e-mails of its owner and active staff; any scope may write.';
