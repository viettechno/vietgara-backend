-- part_barcodes: the scannable codes of a part. A code belongs to the
-- garage (not just the part): it is unique across every part of the
-- garage, case-insensitively, and is matched the same way by lookup and
-- search. unit_id is NULL for the base unit and otherwise points at one
-- of the part's packaging rows (a case barcode).

CREATE TABLE public.part_barcodes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid,
    barcode character varying(64) NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.part_barcodes FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.part_barcodes
    ADD CONSTRAINT part_barcodes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.part_barcodes
    ADD CONSTRAINT part_barcodes_garage_id_id_key UNIQUE (garage_id, id);

CREATE UNIQUE INDEX idx_part_barcodes_garage_barcode_unique ON public.part_barcodes USING btree (garage_id, lower((barcode)::text));

CREATE INDEX idx_part_barcodes_part ON public.part_barcodes USING btree (garage_id, part_id);

ALTER TABLE ONLY public.part_barcodes
    ADD CONSTRAINT part_barcodes_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.part_barcodes
    ADD CONSTRAINT part_barcodes_garage_part_unit_fkey FOREIGN KEY (garage_id, part_id, unit_id) REFERENCES public.part_units(garage_id, part_id, unit_id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_part_barcodes_created_audit BEFORE INSERT ON public.part_barcodes FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.part_barcodes ENABLE ROW LEVEL SECURITY;

CREATE POLICY part_barcodes_rls_policy ON public.part_barcodes USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY part_barcodes_rls_policy ON public.part_barcodes IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
