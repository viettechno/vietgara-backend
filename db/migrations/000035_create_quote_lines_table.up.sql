-- quote_lines: one labor or part line of a quote (garage_id is
-- denormalized from the parent quote so RLS and per-garage indexes apply
-- directly, the same pattern as every other tenant table).

CREATE TYPE public.quote_line_decision AS ENUM (
    'pending',
    'approved',
    'rejected'
);

CREATE TABLE public.quote_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    quote_id uuid NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    item_type public.quote_item_type NOT NULL,
    item_ref_id uuid,
    description text NOT NULL,
    quantity numeric(14,3) DEFAULT 1 NOT NULL,
    unit_price bigint DEFAULT 0 NOT NULL,
    line_total bigint DEFAULT 0 NOT NULL,
    unit_id uuid,
    entered_quantity numeric(14,3),
    decision public.quote_line_decision DEFAULT 'pending'::public.quote_line_decision NOT NULL,
    decided_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid,
    job_id uuid,
    non_billable boolean DEFAULT false NOT NULL
);

ALTER TABLE ONLY public.quote_lines FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.quote_lines
    ADD CONSTRAINT quote_lines_pkey PRIMARY KEY (id);

CREATE INDEX idx_quote_lines_job ON public.quote_lines USING btree (garage_id, job_id) WHERE (job_id IS NOT NULL);

CREATE INDEX idx_quote_lines_quote_id ON public.quote_lines USING btree (garage_id, quote_id, "position");

ALTER TABLE ONLY public.quote_lines
    ADD CONSTRAINT quote_lines_garage_quote_fkey FOREIGN KEY (garage_id, quote_id) REFERENCES public.quotes(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.quote_lines
    ADD CONSTRAINT quote_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_quote_lines_row_audit BEFORE INSERT OR UPDATE ON public.quote_lines FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.quote_lines ENABLE ROW LEVEL SECURITY;

CREATE POLICY quote_lines_rls_policy ON public.quote_lines USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY quote_lines_rls_policy ON public.quote_lines IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
