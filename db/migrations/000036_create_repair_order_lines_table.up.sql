-- repair_order_lines: the approved quote lines carried over by
-- ConvertQuote (FR-QUO-04), or entered directly for an ad-hoc repair
-- order (FR-REP-01, Release 1.3). item_ref_id references an Inventory
-- item for a part line (Release 1.3); left NULL for a part not carried
-- in stock -- it is then simply skipped when stock is deducted on close
-- (Event Design, repair_order.closed).

CREATE TYPE public.repair_order_line_status AS ENUM (
    'not_started',
    'in_progress',
    'completed'
);

CREATE TABLE public.repair_order_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    repair_order_id uuid NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    item_type public.quote_item_type NOT NULL,
    item_ref_id uuid,
    description text NOT NULL,
    quantity numeric(14,3) DEFAULT 1 NOT NULL,
    unit_price bigint DEFAULT 0 NOT NULL,
    line_total bigint DEFAULT 0 NOT NULL,
    unit_id uuid,
    entered_quantity numeric(14,3),
    status public.repair_order_line_status DEFAULT 'not_started'::public.repair_order_line_status NOT NULL,
    assigned_technicians uuid[] DEFAULT '{}'::uuid[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid,
    job_id uuid,
    non_billable boolean DEFAULT false NOT NULL
);

ALTER TABLE ONLY public.repair_order_lines FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.repair_order_lines
    ADD CONSTRAINT repair_order_lines_pkey PRIMARY KEY (id);

CREATE INDEX idx_repair_order_lines_ro_id ON public.repair_order_lines USING btree (garage_id, repair_order_id, "position");

CREATE INDEX idx_repair_order_lines_technicians ON public.repair_order_lines USING gin (assigned_technicians);

ALTER TABLE ONLY public.repair_order_lines
    ADD CONSTRAINT repair_order_lines_garage_ro_fkey FOREIGN KEY (garage_id, repair_order_id) REFERENCES public.repair_orders(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.repair_order_lines
    ADD CONSTRAINT repair_order_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_repair_order_lines_row_audit BEFORE INSERT OR UPDATE ON public.repair_order_lines FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.repair_order_lines ENABLE ROW LEVEL SECURITY;

CREATE POLICY repair_order_lines_rls_policy ON public.repair_order_lines USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY repair_order_lines_rls_policy ON public.repair_order_lines IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
