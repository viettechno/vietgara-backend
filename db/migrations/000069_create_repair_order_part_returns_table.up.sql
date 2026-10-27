-- repair_order_part_returns (FR-INV-11): unused parts coming back from a
-- repair order, immutable. unit_cost is the cost snapshot of the stock issue
-- line they are restored at (base unit).

CREATE TABLE public.repair_order_part_returns (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    repair_order_id uuid NOT NULL,
    repair_order_number character varying(50) DEFAULT ''::character varying NOT NULL,
    stock_issue_id uuid NOT NULL,
    stock_issue_line_id uuid NOT NULL,
    repair_order_line_id uuid NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid,
    entered_quantity numeric(14,3) NOT NULL,
    quantity numeric(14,3) NOT NULL,
    reason text NOT NULL,
    unit_cost numeric(18,4),
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT repair_order_part_returns_entered_quantity_check CHECK ((entered_quantity > (0)::numeric)),
    CONSTRAINT repair_order_part_returns_quantity_check CHECK ((quantity > (0)::numeric))
);

ALTER TABLE ONLY public.repair_order_part_returns FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.repair_order_part_returns
    ADD CONSTRAINT repair_order_part_returns_pkey PRIMARY KEY (id);

CREATE INDEX idx_repair_order_part_returns_line ON public.repair_order_part_returns USING btree (stock_issue_line_id);

CREATE INDEX idx_repair_order_part_returns_order ON public.repair_order_part_returns USING btree (garage_id, repair_order_id, created_at);

CREATE INDEX idx_repair_order_part_returns_part ON public.repair_order_part_returns USING btree (garage_id, part_id);

ALTER TABLE ONLY public.repair_order_part_returns
    ADD CONSTRAINT repair_order_part_returns_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.repair_order_part_returns
    ADD CONSTRAINT repair_order_part_returns_garage_issue_fkey FOREIGN KEY (garage_id, stock_issue_id) REFERENCES public.stock_issues(garage_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.repair_order_part_returns
    ADD CONSTRAINT repair_order_part_returns_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id);

ALTER TABLE ONLY public.repair_order_part_returns
    ADD CONSTRAINT repair_order_part_returns_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.repair_order_part_returns
    ADD CONSTRAINT repair_order_part_returns_issue_line_fkey FOREIGN KEY (stock_issue_line_id) REFERENCES public.stock_issue_lines(id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_repair_order_part_returns_row_audit BEFORE INSERT OR UPDATE ON public.repair_order_part_returns FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.repair_order_part_returns ENABLE ROW LEVEL SECURITY;

CREATE POLICY repair_order_part_returns_rls_policy ON public.repair_order_part_returns USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY repair_order_part_returns_rls_policy ON public.repair_order_part_returns IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
