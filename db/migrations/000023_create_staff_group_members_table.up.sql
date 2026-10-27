-- garage_id is repeated from the group so RLS can filter without a join.

CREATE TABLE public.staff_group_members (
    group_id uuid NOT NULL,
    staff_id uuid NOT NULL,
    garage_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

ALTER TABLE ONLY public.staff_group_members FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.staff_group_members
    ADD CONSTRAINT staff_group_members_pkey PRIMARY KEY (group_id, staff_id);

CREATE INDEX idx_staff_group_members_staff_id ON public.staff_group_members USING btree (staff_id);

ALTER TABLE ONLY public.staff_group_members
    ADD CONSTRAINT staff_group_members_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.staff_group_members
    ADD CONSTRAINT staff_group_members_group_id_fkey FOREIGN KEY (group_id) REFERENCES public.staff_groups(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.staff_group_members
    ADD CONSTRAINT staff_group_members_staff_id_fkey FOREIGN KEY (staff_id) REFERENCES public.garage_staff(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_staff_group_members_created_audit BEFORE INSERT ON public.staff_group_members FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.staff_group_members ENABLE ROW LEVEL SECURITY;

CREATE POLICY staff_group_members_select ON public.staff_group_members FOR SELECT USING (((garage_id = public.app_current_garage_id()) OR (EXISTS ( SELECT 1
   FROM public.garage_staff s
  WHERE ((s.id = staff_group_members.staff_id) AND (s.user_id = public.app_current_user_id()))))));

CREATE POLICY staff_group_members_write ON public.staff_group_members USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

-- Policies of earlier tables that read this one.

CREATE POLICY staff_groups_select ON public.staff_groups FOR SELECT USING (((garage_id = public.app_current_garage_id()) OR (EXISTS ( SELECT 1
   FROM public.garages g
  WHERE ((g.id = staff_groups.garage_id) AND (g.user_id = public.app_current_user_id())))) OR (EXISTS ( SELECT 1
   FROM public.staff_group_members m
  WHERE (m.group_id = staff_groups.id)))));
