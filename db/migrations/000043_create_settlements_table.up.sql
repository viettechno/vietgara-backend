CREATE TYPE public.settlement_status AS ENUM (
    'pending',
    'partially_paid',
    'paid'
);

CREATE TABLE public.settlements (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    repair_order_id uuid NOT NULL,
    customer_name character varying(255) NOT NULL,
    vehicle_license_plate character varying(20) NOT NULL,
    number character varying(50) NOT NULL,
    total_amount bigint DEFAULT 0 NOT NULL,
    paid_amount bigint DEFAULT 0 NOT NULL,
    issuer jsonb DEFAULT '{}'::jsonb NOT NULL,
    billing_party jsonb DEFAULT '{}'::jsonb NOT NULL,
    invoice_requested boolean DEFAULT false NOT NULL,
    status public.settlement_status DEFAULT 'pending'::public.settlement_status NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.settlements FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.settlements
    ADD CONSTRAINT settlements_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.settlements
    ADD CONSTRAINT settlements_garage_id_id_key UNIQUE (garage_id, id);

ALTER TABLE ONLY public.settlements
    ADD CONSTRAINT settlements_garage_number_unique UNIQUE (garage_id, number);

ALTER TABLE ONLY public.settlements
    ADD CONSTRAINT settlements_repair_order_id_unique UNIQUE (repair_order_id);

CREATE INDEX idx_settlements_billing_profile ON public.settlements USING btree (garage_id, ((billing_party ->> 'tax_profile_id'::text))) WHERE ((billing_party ->> 'tax_profile_id'::text) IS NOT NULL);

CREATE INDEX idx_settlements_garage_status_created_at ON public.settlements USING btree (garage_id, status, created_at DESC);

ALTER TABLE ONLY public.settlements
    ADD CONSTRAINT settlements_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.settlements
    ADD CONSTRAINT settlements_garage_ro_fkey FOREIGN KEY (garage_id, repair_order_id) REFERENCES public.repair_orders(garage_id, id);

CREATE TRIGGER trigger_settlements_row_audit BEFORE INSERT OR UPDATE ON public.settlements FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.settlements ENABLE ROW LEVEL SECURITY;

CREATE POLICY settlements_rls_policy ON public.settlements USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY settlements_rls_policy ON public.settlements IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
