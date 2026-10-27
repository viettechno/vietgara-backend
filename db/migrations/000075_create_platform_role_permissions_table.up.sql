CREATE TYPE public.platform_permission AS ENUM (
    'plan.read',
    'plan.write',
    'subscription.read',
    'subscription.write',
    'stats.read',
    'garage_directory.read',
    'user.read',
    'platform_staff.read',
    'platform_staff.write',
    'support.read',
    'support.write',
    'audit.read'
);

CREATE TABLE public.platform_role_permissions (
    role_id uuid NOT NULL,
    permission public.platform_permission NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

ALTER TABLE ONLY public.platform_role_permissions
    ADD CONSTRAINT platform_role_permissions_pkey PRIMARY KEY (role_id, permission);

ALTER TABLE ONLY public.platform_role_permissions
    ADD CONSTRAINT platform_role_permissions_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.platform_roles(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_platform_role_permissions_created_audit BEFORE INSERT ON public.platform_role_permissions FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();
