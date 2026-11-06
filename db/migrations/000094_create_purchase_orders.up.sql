-- Purchase orders (Release 13.0, FR-INV-16): a numbered order to one supplier
-- that can be e-mailed and received into a goods receipt. Quantities follow
-- the units of goods receipts: entered_* is what was typed (in unit_id, NULL
-- for the base unit), quantity is the base-unit amount, received_quantity the
-- base-unit amount delivered by completed goods receipts.

CREATE TYPE public.purchase_order_status AS ENUM (
    'draft',
    'sent',
    'partially_received',
    'received',
    'cancelled'
);

CREATE TABLE public.purchase_orders (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    number character varying(50) NOT NULL,
    status public.purchase_order_status DEFAULT 'draft'::public.purchase_order_status NOT NULL,
    supplier_id uuid NOT NULL,
    order_date date NOT NULL,
    expected_date date,
    notes text DEFAULT ''::text NOT NULL,
    sent_at timestamp with time zone,
    sent_to character varying(255) DEFAULT ''::character varying NOT NULL,
    cancelled_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.purchase_orders FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.purchase_orders
    ADD CONSTRAINT purchase_orders_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.purchase_orders
    ADD CONSTRAINT purchase_orders_garage_id_id_key UNIQUE (garage_id, id);

ALTER TABLE ONLY public.purchase_orders
    ADD CONSTRAINT purchase_orders_garage_number_key UNIQUE (garage_id, number);

CREATE INDEX idx_purchase_orders_list ON public.purchase_orders USING btree (garage_id, created_at DESC);

CREATE INDEX idx_purchase_orders_supplier ON public.purchase_orders USING btree (garage_id, supplier_id);

ALTER TABLE ONLY public.purchase_orders
    ADD CONSTRAINT purchase_orders_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.purchase_orders
    ADD CONSTRAINT purchase_orders_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id) REFERENCES public.suppliers(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_purchase_orders_row_audit BEFORE INSERT OR UPDATE ON public.purchase_orders FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.purchase_orders ENABLE ROW LEVEL SECURITY;

CREATE POLICY purchase_orders_rls_policy ON public.purchase_orders USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY purchase_orders_rls_policy ON public.purchase_orders IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

CREATE TABLE public.purchase_order_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    purchase_order_id uuid NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    part_id uuid NOT NULL,
    unit_id uuid,
    entered_quantity numeric(14,3) NOT NULL,
    quantity numeric(14,3) NOT NULL,
    received_quantity numeric(14,3) DEFAULT 0 NOT NULL,
    unit_cost bigint DEFAULT 0 NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT purchase_order_lines_entered_quantity_check CHECK ((entered_quantity > (0)::numeric)),
    CONSTRAINT purchase_order_lines_quantity_check CHECK ((quantity > (0)::numeric)),
    CONSTRAINT purchase_order_lines_received_quantity_check CHECK ((received_quantity >= (0)::numeric)),
    CONSTRAINT purchase_order_lines_unit_cost_check CHECK ((unit_cost >= 0))
);

ALTER TABLE ONLY public.purchase_order_lines FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.purchase_order_lines
    ADD CONSTRAINT purchase_order_lines_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.purchase_order_lines
    ADD CONSTRAINT purchase_order_lines_order_part_key UNIQUE (purchase_order_id, part_id);

CREATE INDEX idx_purchase_order_lines_order ON public.purchase_order_lines USING btree (garage_id, purchase_order_id, "position");

CREATE INDEX idx_purchase_order_lines_part ON public.purchase_order_lines USING btree (garage_id, part_id);

ALTER TABLE ONLY public.purchase_order_lines
    ADD CONSTRAINT purchase_order_lines_garage_order_fkey FOREIGN KEY (garage_id, purchase_order_id) REFERENCES public.purchase_orders(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.purchase_order_lines
    ADD CONSTRAINT purchase_order_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id);

ALTER TABLE ONLY public.purchase_order_lines
    ADD CONSTRAINT purchase_order_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_purchase_order_lines_row_audit BEFORE INSERT OR UPDATE ON public.purchase_order_lines FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.purchase_order_lines ENABLE ROW LEVEL SECURITY;

CREATE POLICY purchase_order_lines_rls_policy ON public.purchase_order_lines USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY purchase_order_lines_rls_policy ON public.purchase_order_lines IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- A goods receipt can be received against an order.
ALTER TABLE public.goods_receipts ADD COLUMN purchase_order_id uuid;

ALTER TABLE ONLY public.goods_receipts
    ADD CONSTRAINT goods_receipts_garage_purchase_order_fkey FOREIGN KEY (garage_id, purchase_order_id) REFERENCES public.purchase_orders(garage_id, id) ON DELETE RESTRICT;

CREATE INDEX idx_goods_receipts_purchase_order ON public.goods_receipts USING btree (garage_id, purchase_order_id) WHERE (purchase_order_id IS NOT NULL);
