-- Optional TOTP second factor (FR-IAM-09, NFR-SE-04). A user_mfa row exists
-- from the start of the enrolment; the factor counts only once enabled_at
-- is set by confirming a code. The secret is encrypted by the application
-- (AES-GCM) and never stored in clear.

CREATE TABLE public.user_mfa (
    user_id uuid NOT NULL,
    secret_encrypted bytea NOT NULL,
    enabled_at timestamp with time zone,
    last_used_step bigint DEFAULT 0 NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.user_mfa
    ADD CONSTRAINT user_mfa_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY public.user_mfa
    ADD CONSTRAINT user_mfa_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_user_mfa_row_audit BEFORE INSERT OR UPDATE ON public.user_mfa FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();
