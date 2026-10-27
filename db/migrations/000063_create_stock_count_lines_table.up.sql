-- stock_count_lines: one part of a count. system_quantity is the stock on hand
-- (base unit, may be negative) when the line was last counted; counted_* are
-- NULL while the line is uncounted. average_cost is the cost the difference is
-- valued at, written when the count completes (a draft shows the live one).

CREATE TABLE public.stock_count_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    stock_count_id uuid NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid,
    entered_counted_quantity numeric(14,3),
    counted_quantity numeric(14,3),
    system_quantity numeric(14,3) DEFAULT 0 NOT NULL,
    average_cost numeric(18,4),
    counted_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT stock_count_lines_counted_quantity_check CHECK ((counted_quantity >= (0)::numeric)),
    CONSTRAINT stock_count_lines_counted_together CHECK (((counted_quantity IS NULL) = (entered_counted_quantity IS NULL))),
    CONSTRAINT stock_count_lines_entered_counted_quantity_check CHECK ((entered_counted_quantity >= (0)::numeric))
);

ALTER TABLE ONLY public.stock_count_lines FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.stock_count_lines
    ADD CONSTRAINT stock_count_lines_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.stock_count_lines
    ADD CONSTRAINT stock_count_lines_count_part_unique UNIQUE (stock_count_id, part_id);

CREATE INDEX idx_stock_count_lines_count ON public.stock_count_lines USING btree (garage_id, stock_count_id, "position");

CREATE INDEX idx_stock_count_lines_part ON public.stock_count_lines USING btree (garage_id, part_id);

ALTER TABLE ONLY public.stock_count_lines
    ADD CONSTRAINT stock_count_lines_garage_count_fkey FOREIGN KEY (garage_id, stock_count_id) REFERENCES public.stock_counts(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.stock_count_lines
    ADD CONSTRAINT stock_count_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id);

ALTER TABLE ONLY public.stock_count_lines
    ADD CONSTRAINT stock_count_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_stock_count_lines_row_audit BEFORE INSERT OR UPDATE ON public.stock_count_lines FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.stock_count_lines ENABLE ROW LEVEL SECURITY;

CREATE POLICY stock_count_lines_rls_policy ON public.stock_count_lines USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY stock_count_lines_rls_policy ON public.stock_count_lines IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
