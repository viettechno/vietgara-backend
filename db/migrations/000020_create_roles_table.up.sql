-- roles: defined by an owner, shared by all their garages. default_key
-- marks the roles the platform generated, which keeps that step idempotent.

CREATE TABLE public.roles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    owner_user_id uuid NOT NULL,
    name character varying(100) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    default_key public.default_role,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.roles FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX idx_roles_owner_default_unique ON public.roles USING btree (owner_user_id, default_key) WHERE (default_key IS NOT NULL);

CREATE UNIQUE INDEX idx_roles_owner_name_unique ON public.roles USING btree (owner_user_id, lower((name)::text));

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_roles_row_audit BEFORE INSERT OR UPDATE ON public.roles FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.roles ENABLE ROW LEVEL SECURITY;

CREATE POLICY roles_write ON public.roles USING ((owner_user_id = public.app_current_user_id())) WITH CHECK ((owner_user_id = public.app_current_user_id()));
