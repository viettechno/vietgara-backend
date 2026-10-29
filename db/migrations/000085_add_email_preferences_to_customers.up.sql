-- Notification (Release 10.1): the language of a customer's e-mails and the
-- moment the customer opted out of the non-essential ones.

ALTER TABLE public.customers
    ADD COLUMN locale character varying(2) DEFAULT 'vi'::character varying NOT NULL,
    ADD COLUMN email_opt_out_at timestamp with time zone,
    ADD CONSTRAINT customers_locale_check CHECK (((locale)::text = ANY ((ARRAY['vi'::character varying, 'en'::character varying])::text[])));
