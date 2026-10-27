-- stock_counts: a numbered count of parts (FR-INV-12). A draft is counted
-- line by line; completing it posts the adjustments and freezes it.

CREATE TYPE public.stock_count_status AS ENUM (
    'draft',
    'completed',
    'cancelled'
);

CREATE TABLE public.stock_counts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    number character varying(50) NOT NULL,
    name character varying(255) DEFAULT ''::character varying NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    status public.stock_count_status DEFAULT 'draft'::public.stock_count_status NOT NULL,
    completed_at timestamp with time zone,
    completed_by uuid,
    cancelled_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.stock_counts FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.stock_counts
    ADD CONSTRAINT stock_counts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.stock_counts
    ADD CONSTRAINT stock_counts_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_stock_counts_garage_created_at ON public.stock_counts USING btree (garage_id, created_at DESC);

CREATE UNIQUE INDEX idx_stock_counts_garage_number_unique ON public.stock_counts USING btree (garage_id, number);

ALTER TABLE ONLY public.stock_counts
    ADD CONSTRAINT stock_counts_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_stock_counts_row_audit BEFORE INSERT OR UPDATE ON public.stock_counts FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.stock_counts ENABLE ROW LEVEL SECURITY;

CREATE POLICY stock_counts_rls_policy ON public.stock_counts USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY stock_counts_rls_policy ON public.stock_counts IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
