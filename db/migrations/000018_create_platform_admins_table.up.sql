-- platform_admins: operators who manage subscription plans (FR-LIC-05,
-- FR-IAM-08). Synchronized at startup from PLATFORM_ADMIN_EMAILS.

CREATE TABLE public.platform_admins (
    user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

ALTER TABLE ONLY public.platform_admins
    ADD CONSTRAINT platform_admins_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY public.platform_admins
    ADD CONSTRAINT platform_admins_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_platform_admins_created_audit BEFORE INSERT ON public.platform_admins FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();
