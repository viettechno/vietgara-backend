-- unit_cost is the average-cost snapshot (VND per base unit) taken when the
-- issue is posted; NULL while a draft or when no cost was known.

CREATE TABLE public.stock_issue_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    stock_issue_id uuid NOT NULL,
    repair_order_line_id uuid NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid,
    entered_quantity numeric(14,3) NOT NULL,
    quantity numeric(14,3) NOT NULL,
    unit_cost numeric(18,4),
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT stock_issue_lines_quantity_check CHECK ((quantity > (0)::numeric))
);

ALTER TABLE ONLY public.stock_issue_lines FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.stock_issue_lines
    ADD CONSTRAINT stock_issue_lines_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.stock_issue_lines
    ADD CONSTRAINT stock_issue_lines_issue_line_unique UNIQUE (stock_issue_id, repair_order_line_id);

CREATE INDEX idx_stock_issue_lines_issue ON public.stock_issue_lines USING btree (garage_id, stock_issue_id);

CREATE INDEX idx_stock_issue_lines_part ON public.stock_issue_lines USING btree (garage_id, part_id);

ALTER TABLE ONLY public.stock_issue_lines
    ADD CONSTRAINT stock_issue_lines_garage_issue_fkey FOREIGN KEY (garage_id, stock_issue_id) REFERENCES public.stock_issues(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.stock_issue_lines
    ADD CONSTRAINT stock_issue_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id);

ALTER TABLE ONLY public.stock_issue_lines
    ADD CONSTRAINT stock_issue_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_stock_issue_lines_row_audit BEFORE INSERT OR UPDATE ON public.stock_issue_lines FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.stock_issue_lines ENABLE ROW LEVEL SECURITY;

CREATE POLICY stock_issue_lines_rls_policy ON public.stock_issue_lines USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY stock_issue_lines_rls_policy ON public.stock_issue_lines IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
