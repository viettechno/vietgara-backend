-- stock_issues: the numbered stock-out document of a repair order
-- (FR-INV-20), one per order. A draft follows the order's part lines and is
-- the reservation; posting writes the stock movements.

CREATE TYPE public.stock_issue_status AS ENUM (
    'draft',
    'posted',
    'cancelled'
);

CREATE TABLE public.stock_issues (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    number character varying(50) NOT NULL,
    repair_order_id uuid NOT NULL,
    repair_order_number character varying(50) DEFAULT ''::character varying NOT NULL,
    status public.stock_issue_status DEFAULT 'draft'::public.stock_issue_status NOT NULL,
    posted_at timestamp with time zone,
    posted_by uuid,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    is_short boolean DEFAULT false NOT NULL
);

ALTER TABLE ONLY public.stock_issues FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.stock_issues
    ADD CONSTRAINT stock_issues_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.stock_issues
    ADD CONSTRAINT stock_issues_garage_id_id_key UNIQUE (garage_id, id);

CREATE UNIQUE INDEX idx_stock_issues_garage_number_unique ON public.stock_issues USING btree (garage_id, number);

CREATE UNIQUE INDEX idx_stock_issues_garage_repair_order_unique ON public.stock_issues USING btree (garage_id, repair_order_id);

ALTER TABLE ONLY public.stock_issues
    ADD CONSTRAINT stock_issues_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_stock_issues_row_audit BEFORE INSERT OR UPDATE ON public.stock_issues FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.stock_issues ENABLE ROW LEVEL SECURITY;

CREATE POLICY stock_issues_rls_policy ON public.stock_issues USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY stock_issues_rls_policy ON public.stock_issues IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
