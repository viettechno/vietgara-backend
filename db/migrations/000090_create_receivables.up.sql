-- Receivables (Release 13.0, FR-REC-01, 03, 04): what customers owe a garage,
-- with a state of its own fed by the events of Billing & Settlement.
-- receivable_entries is its ledger; receivable_reminders logs each scheduled
-- reminder once.

CREATE TYPE public.receivable_status AS ENUM (
    'open',
    'paid'
);

CREATE TYPE public.receivable_entry_kind AS ENUM (
    'charge',
    'payment',
    'void'
);

CREATE TYPE public.reminder_outcome AS ENUM (
    'sent',
    'skipped',
    'failed'
);

CREATE TABLE public.receivable_settings (
    garage_id uuid NOT NULL,
    payment_terms_days integer DEFAULT 7 NOT NULL,
    reminders_enabled boolean DEFAULT true NOT NULL,
    reminder_offset_days integer[] DEFAULT '{3,7}'::integer[] NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT receivable_settings_terms_check CHECK (((payment_terms_days >= 0) AND (payment_terms_days <= 365))),
    CONSTRAINT receivable_settings_offsets_check CHECK (((cardinality(reminder_offset_days) <= 5) AND (1 <= ALL (reminder_offset_days)) AND (90 >= ALL (reminder_offset_days))))
);

ALTER TABLE ONLY public.receivable_settings FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.receivable_settings
    ADD CONSTRAINT receivable_settings_pkey PRIMARY KEY (garage_id);

ALTER TABLE ONLY public.receivable_settings
    ADD CONSTRAINT receivable_settings_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_receivable_settings_row_audit BEFORE INSERT OR UPDATE ON public.receivable_settings FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.receivable_settings ENABLE ROW LEVEL SECURITY;

CREATE POLICY receivable_settings_rls_policy ON public.receivable_settings USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY receivable_settings_rls_policy ON public.receivable_settings IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

CREATE TABLE public.receivables (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    settlement_id uuid NOT NULL,
    settlement_number character varying(50) NOT NULL,
    customer_id uuid,
    customer_name character varying(255) NOT NULL,
    vehicle_license_plate character varying(20) NOT NULL,
    total_amount bigint NOT NULL,
    paid_amount bigint DEFAULT 0 NOT NULL,
    due_date date NOT NULL,
    status public.receivable_status DEFAULT 'open'::public.receivable_status NOT NULL,
    reminders_sent integer DEFAULT 0 NOT NULL,
    last_reminder_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT receivables_amounts_check CHECK (((total_amount >= 0) AND (paid_amount >= 0)))
);

ALTER TABLE ONLY public.receivables FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.receivables
    ADD CONSTRAINT receivables_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.receivables
    ADD CONSTRAINT receivables_garage_id_id_key UNIQUE (garage_id, id);

ALTER TABLE ONLY public.receivables
    ADD CONSTRAINT receivables_garage_settlement_unique UNIQUE (garage_id, settlement_id);

CREATE INDEX idx_receivables_status_due ON public.receivables USING btree (garage_id, status, due_date);

CREATE INDEX idx_receivables_customer ON public.receivables USING btree (garage_id, customer_id);

ALTER TABLE ONLY public.receivables
    ADD CONSTRAINT receivables_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_receivables_row_audit BEFORE INSERT OR UPDATE ON public.receivables FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.receivables ENABLE ROW LEVEL SECURITY;

CREATE POLICY receivables_rls_policy ON public.receivables USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY receivables_rls_policy ON public.receivables IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

CREATE TABLE public.receivable_entries (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    receivable_id uuid NOT NULL,
    kind public.receivable_entry_kind NOT NULL,
    amount bigint NOT NULL,
    reference character varying(50) NOT NULL,
    method public.payment_method,
    source_key character varying(100) NOT NULL,
    occurred_at timestamp with time zone NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT receivable_entries_amount_check CHECK ((amount > 0))
);

ALTER TABLE ONLY public.receivable_entries FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.receivable_entries
    ADD CONSTRAINT receivable_entries_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.receivable_entries
    ADD CONSTRAINT receivable_entries_source_unique UNIQUE (garage_id, source_key);

CREATE INDEX idx_receivable_entries_receivable ON public.receivable_entries USING btree (garage_id, receivable_id, occurred_at);

CREATE INDEX idx_receivable_entries_occurred ON public.receivable_entries USING btree (garage_id, occurred_at);

ALTER TABLE ONLY public.receivable_entries
    ADD CONSTRAINT receivable_entries_garage_receivable_fkey FOREIGN KEY (garage_id, receivable_id) REFERENCES public.receivables(garage_id, id) ON DELETE CASCADE;

CREATE TRIGGER trigger_receivable_entries_created_audit BEFORE INSERT ON public.receivable_entries FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.receivable_entries ENABLE ROW LEVEL SECURITY;

CREATE POLICY receivable_entries_rls_policy ON public.receivable_entries USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY receivable_entries_rls_policy ON public.receivable_entries IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

CREATE TABLE public.receivable_reminders (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    receivable_id uuid NOT NULL,
    offset_days integer NOT NULL,
    channel character varying(10) DEFAULT 'email'::character varying NOT NULL,
    outcome public.reminder_outcome NOT NULL,
    reason character varying(20),
    attempts integer DEFAULT 1 NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT receivable_reminders_offset_check CHECK ((offset_days > 0))
);

ALTER TABLE ONLY public.receivable_reminders FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.receivable_reminders
    ADD CONSTRAINT receivable_reminders_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.receivable_reminders
    ADD CONSTRAINT receivable_reminders_offset_unique UNIQUE (receivable_id, offset_days);

ALTER TABLE ONLY public.receivable_reminders
    ADD CONSTRAINT receivable_reminders_garage_receivable_fkey FOREIGN KEY (garage_id, receivable_id) REFERENCES public.receivables(garage_id, id) ON DELETE CASCADE;

CREATE TRIGGER trigger_receivable_reminders_row_audit BEFORE INSERT OR UPDATE ON public.receivable_reminders FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.receivable_reminders ENABLE ROW LEVEL SECURITY;

CREATE POLICY receivable_reminders_rls_policy ON public.receivable_reminders USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY receivable_reminders_rls_policy ON public.receivable_reminders IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- The reminder job starts from the garages that have something to remind
-- about; it cannot list them under row-level security, so this function
-- answers with the garage ids only (the same pattern as
-- find_quote_by_public_token()).
CREATE FUNCTION public.receivable_reminder_garages() RETURNS SETOF uuid
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public'
    AS $$
    SELECT DISTINCT garage_id FROM public.receivables WHERE status = 'open' AND customer_id IS NOT NULL;
$$;

REVOKE ALL ON FUNCTION public.receivable_reminder_garages() FROM PUBLIC;
GRANT ALL ON FUNCTION public.receivable_reminder_garages() TO vietgara_app;
