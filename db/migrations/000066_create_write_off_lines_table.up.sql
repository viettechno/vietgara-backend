CREATE TABLE public.write_off_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    write_off_id uuid NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid,
    entered_quantity numeric(14,3) NOT NULL,
    quantity numeric(14,3) NOT NULL,
    unit_cost numeric(18,4),
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT write_off_lines_entered_quantity_check CHECK ((entered_quantity > (0)::numeric)),
    CONSTRAINT write_off_lines_quantity_check CHECK ((quantity > (0)::numeric))
);

ALTER TABLE ONLY public.write_off_lines FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.write_off_lines
    ADD CONSTRAINT write_off_lines_pkey PRIMARY KEY (id);

CREATE INDEX idx_write_off_lines_part ON public.write_off_lines USING btree (garage_id, part_id);

CREATE INDEX idx_write_off_lines_write_off ON public.write_off_lines USING btree (garage_id, write_off_id, "position");

ALTER TABLE ONLY public.write_off_lines
    ADD CONSTRAINT write_off_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id);

ALTER TABLE ONLY public.write_off_lines
    ADD CONSTRAINT write_off_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.write_off_lines
    ADD CONSTRAINT write_off_lines_garage_write_off_fkey FOREIGN KEY (garage_id, write_off_id) REFERENCES public.write_offs(garage_id, id) ON DELETE CASCADE;

CREATE TRIGGER trigger_write_off_lines_row_audit BEFORE INSERT OR UPDATE ON public.write_off_lines FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.write_off_lines ENABLE ROW LEVEL SECURITY;

CREATE POLICY write_off_lines_rls_policy ON public.write_off_lines USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY write_off_lines_rls_policy ON public.write_off_lines IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
