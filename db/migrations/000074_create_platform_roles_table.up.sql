CREATE TABLE public.platform_roles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(100) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid
);

ALTER TABLE ONLY public.platform_roles
    ADD CONSTRAINT platform_roles_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX idx_platform_roles_name ON public.platform_roles USING btree (lower((name)::text));

CREATE TRIGGER trigger_platform_roles_row_audit BEFORE INSERT OR UPDATE ON public.platform_roles FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();
