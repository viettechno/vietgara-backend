-- entitlements: per-user feature switches derived from the
-- subscription's plan and status (source of the entitlement cache).

CREATE TABLE public.entitlements (
    user_id uuid NOT NULL,
    feature public.plan_feature NOT NULL,
    enabled boolean NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid
);

ALTER TABLE ONLY public.entitlements
    ADD CONSTRAINT entitlements_pkey PRIMARY KEY (user_id, feature);

ALTER TABLE ONLY public.entitlements
    ADD CONSTRAINT entitlements_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_entitlements_row_audit BEFORE INSERT OR UPDATE ON public.entitlements FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();
