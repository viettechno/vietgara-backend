CREATE TABLE public.platform_staff_groups (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(100) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    role_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid
);

ALTER TABLE ONLY public.platform_staff_groups
    ADD CONSTRAINT platform_staff_groups_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX idx_platform_staff_groups_name ON public.platform_staff_groups USING btree (lower((name)::text));

CREATE INDEX idx_platform_staff_groups_role_id ON public.platform_staff_groups USING btree (role_id);

ALTER TABLE ONLY public.platform_staff_groups
    ADD CONSTRAINT platform_staff_groups_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.platform_roles(id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_platform_staff_groups_row_audit BEFORE INSERT OR UPDATE ON public.platform_staff_groups FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();
