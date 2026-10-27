-- staff_groups: members of one garage sharing a role.

CREATE TABLE public.staff_groups (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    name character varying(100) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    role_id uuid NOT NULL,
    default_key public.default_role,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.staff_groups FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.staff_groups
    ADD CONSTRAINT staff_groups_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX idx_staff_groups_garage_default_unique ON public.staff_groups USING btree (garage_id, default_key) WHERE (default_key IS NOT NULL);

CREATE UNIQUE INDEX idx_staff_groups_garage_name_unique ON public.staff_groups USING btree (garage_id, lower((name)::text));

CREATE INDEX idx_staff_groups_role_id ON public.staff_groups USING btree (role_id);

ALTER TABLE ONLY public.staff_groups
    ADD CONSTRAINT staff_groups_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.staff_groups
    ADD CONSTRAINT staff_groups_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id);

CREATE TRIGGER trigger_staff_groups_row_audit BEFORE INSERT OR UPDATE ON public.staff_groups FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.staff_groups ENABLE ROW LEVEL SECURITY;

CREATE POLICY staff_groups_write ON public.staff_groups USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

-- Policies of earlier tables that read this one.

CREATE POLICY roles_select ON public.roles FOR SELECT USING (((owner_user_id = public.app_current_user_id()) OR (owner_user_id = ( SELECT g.user_id
   FROM public.garages g
  WHERE (g.id = public.app_current_garage_id()))) OR (EXISTS ( SELECT 1
   FROM public.staff_groups sg
  WHERE (sg.role_id = roles.id)))));
