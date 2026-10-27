-- One-time recovery codes; only a hash is kept, used_at marks a spent code.

CREATE TABLE public.user_mfa_recovery_codes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    code_hash text NOT NULL,
    used_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.user_mfa_recovery_codes
    ADD CONSTRAINT user_mfa_recovery_codes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.user_mfa_recovery_codes
    ADD CONSTRAINT user_mfa_recovery_codes_unique UNIQUE (user_id, code_hash);

ALTER TABLE ONLY public.user_mfa_recovery_codes
    ADD CONSTRAINT user_mfa_recovery_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_user_mfa_recovery_codes_row_audit BEFORE INSERT OR UPDATE ON public.user_mfa_recovery_codes FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();
