-- write_offs and write_off_lines (FR-INV-09): stock leaves at average cost for
-- a reason; unit_cost is the average-cost snapshot per base unit.

CREATE TYPE public.write_off_reason AS ENUM (
    'damaged',
    'lost',
    'expired',
    'internal_use',
    'warranty_replacement'
);

CREATE TABLE public.write_offs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    number character varying(50) NOT NULL,
    status public.adjustment_status DEFAULT 'completed'::public.adjustment_status NOT NULL,
    reason public.write_off_reason NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    cancelled_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.write_offs FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.write_offs
    ADD CONSTRAINT write_offs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.write_offs
    ADD CONSTRAINT write_offs_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_write_offs_garage_created_at ON public.write_offs USING btree (garage_id, created_at DESC);

CREATE UNIQUE INDEX idx_write_offs_garage_number_unique ON public.write_offs USING btree (garage_id, number);

ALTER TABLE ONLY public.write_offs
    ADD CONSTRAINT write_offs_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_write_offs_row_audit BEFORE INSERT OR UPDATE ON public.write_offs FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.write_offs ENABLE ROW LEVEL SECURITY;

CREATE POLICY write_offs_rls_policy ON public.write_offs USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY write_offs_rls_policy ON public.write_offs IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
