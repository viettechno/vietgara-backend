-- The platform's payment details: one row.

CREATE TABLE public.platform_payment_settings (
    id smallint DEFAULT 1 NOT NULL,
    bank_name character varying(255) DEFAULT ''::character varying NOT NULL,
    account_number character varying(100) DEFAULT ''::character varying NOT NULL,
    account_holder character varying(255) DEFAULT ''::character varying NOT NULL,
    branch character varying(255) DEFAULT ''::character varying NOT NULL,
    instructions text DEFAULT ''::text NOT NULL,
    qr_file_id uuid,
    last_review_reminder_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT platform_payment_settings_id_check CHECK ((id = 1))
);

ALTER TABLE ONLY public.platform_payment_settings
    ADD CONSTRAINT platform_payment_settings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.platform_payment_settings
    ADD CONSTRAINT platform_payment_settings_qr_file_id_fkey FOREIGN KEY (qr_file_id) REFERENCES public.files(id);

CREATE TRIGGER trigger_platform_payment_settings_row_audit BEFORE INSERT OR UPDATE ON public.platform_payment_settings FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

INSERT INTO platform_payment_settings (id) VALUES (1);
