-- part_categories: the category tree of at most three levels (FR-CAT-03).
-- The depth is enforced by the use case; parent and child share a garage
-- through the composite foreign key.

CREATE TABLE public.part_categories (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    parent_id uuid,
    name character varying(255) NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);

ALTER TABLE ONLY public.part_categories FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.part_categories
    ADD CONSTRAINT part_categories_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.part_categories
    ADD CONSTRAINT part_categories_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_part_categories_garage_parent ON public.part_categories USING btree (garage_id, parent_id) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX idx_part_categories_sibling_name_unique ON public.part_categories USING btree (garage_id, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), lower((name)::text)) WHERE (deleted_at IS NULL);

ALTER TABLE ONLY public.part_categories
    ADD CONSTRAINT part_categories_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.part_categories
    ADD CONSTRAINT part_categories_garage_parent_fkey FOREIGN KEY (garage_id, parent_id) REFERENCES public.part_categories(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_part_categories_row_audit BEFORE INSERT OR UPDATE ON public.part_categories FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.part_categories ENABLE ROW LEVEL SECURITY;

CREATE POLICY part_categories_rls_policy ON public.part_categories USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY part_categories_rls_policy ON public.part_categories IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
