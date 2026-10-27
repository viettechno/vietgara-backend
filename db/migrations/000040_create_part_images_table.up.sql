-- part_images: the one to three images of a part (FR-CAT-04), files of kind
-- part_image in display order. file_id is checked by the use case (the File
-- module is another module, so there is no foreign key).

CREATE TABLE public.part_images (
    garage_id uuid NOT NULL,
    part_id uuid NOT NULL,
    file_id uuid NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.part_images FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.part_images
    ADD CONSTRAINT part_images_pkey PRIMARY KEY (garage_id, part_id, file_id);

CREATE INDEX idx_part_images_part ON public.part_images USING btree (garage_id, part_id, "position");

ALTER TABLE ONLY public.part_images
    ADD CONSTRAINT part_images_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id) ON DELETE CASCADE;

CREATE TRIGGER trigger_part_images_created_audit BEFORE INSERT ON public.part_images FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.part_images ENABLE ROW LEVEL SECURITY;

CREATE POLICY part_images_rls_policy ON public.part_images USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY part_images_rls_policy ON public.part_images IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
