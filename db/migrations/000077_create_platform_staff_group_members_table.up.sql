CREATE TABLE public.platform_staff_group_members (
    group_id uuid NOT NULL,
    user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid
);

ALTER TABLE ONLY public.platform_staff_group_members
    ADD CONSTRAINT platform_staff_group_members_pkey PRIMARY KEY (group_id, user_id);

CREATE INDEX idx_platform_staff_group_members_user_id ON public.platform_staff_group_members USING btree (user_id);

ALTER TABLE ONLY public.platform_staff_group_members
    ADD CONSTRAINT platform_staff_group_members_group_id_fkey FOREIGN KEY (group_id) REFERENCES public.platform_staff_groups(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.platform_staff_group_members
    ADD CONSTRAINT platform_staff_group_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_platform_staff_group_members_created_audit BEFORE INSERT ON public.platform_staff_group_members FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();
