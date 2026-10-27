CREATE TYPE public.repair_order_status AS ENUM (
    'open',
    'in_progress',
    'waiting_on_parts',
    'completed',
    'closed',
    'cancelled'
);

CREATE TABLE public.repair_orders (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    quote_id uuid,
    vehicle_id uuid NOT NULL,
    customer_name character varying(255) NOT NULL,
    vehicle_license_plate character varying(20) NOT NULL,
    number character varying(50) NOT NULL,
    status public.repair_order_status DEFAULT 'open'::public.repair_order_status NOT NULL,
    total_amount bigint DEFAULT 0 NOT NULL,
    issuer jsonb DEFAULT '{}'::jsonb NOT NULL,
    billing_party jsonb DEFAULT '{}'::jsonb NOT NULL,
    closed_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    waiting_on_parts boolean DEFAULT false NOT NULL
);

ALTER TABLE ONLY public.repair_orders FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.repair_orders
    ADD CONSTRAINT repair_orders_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.repair_orders
    ADD CONSTRAINT repair_orders_garage_id_id_key UNIQUE (garage_id, id);

ALTER TABLE ONLY public.repair_orders
    ADD CONSTRAINT repair_orders_quote_id_unique UNIQUE (quote_id);

CREATE UNIQUE INDEX idx_repair_orders_garage_number_unique ON public.repair_orders USING btree (garage_id, number) WHERE (deleted_at IS NULL);

CREATE INDEX idx_repair_orders_garage_status_created_at ON public.repair_orders USING btree (garage_id, status, created_at DESC) WHERE (deleted_at IS NULL);

CREATE INDEX idx_repair_orders_vehicle_id ON public.repair_orders USING btree (garage_id, vehicle_id, created_at DESC) WHERE (deleted_at IS NULL);

ALTER TABLE ONLY public.repair_orders
    ADD CONSTRAINT repair_orders_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.repair_orders
    ADD CONSTRAINT repair_orders_garage_quote_fkey FOREIGN KEY (garage_id, quote_id) REFERENCES public.quotes(garage_id, id);

ALTER TABLE ONLY public.repair_orders
    ADD CONSTRAINT repair_orders_garage_vehicle_fkey FOREIGN KEY (garage_id, vehicle_id) REFERENCES public.vehicles(garage_id, id);

CREATE TRIGGER trigger_repair_orders_row_audit BEFORE INSERT OR UPDATE ON public.repair_orders FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.repair_orders ENABLE ROW LEVEL SECURITY;

CREATE POLICY repair_orders_rls_policy ON public.repair_orders USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY repair_orders_rls_policy ON public.repair_orders IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
