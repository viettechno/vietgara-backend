-- stock_transactions: an append-only log of every stock movement
-- (FR-INV-01/02/05). quantity is always positive; type gives the
-- direction. quantity is NUMERIC(14,3) like every other quantity, so the
-- valuation and profit reports can aggregate it in SQL.

CREATE TYPE public.stock_transaction_ref_type AS ENUM (
    'purchase',
    'repair_order',
    'goods_receipt',
    'stock_issue',
    'stock_count',
    'write_off',
    'supplier_return',
    'repair_order_return'
);

CREATE TYPE public.stock_transaction_type AS ENUM (
    'in',
    'out'
);

CREATE TABLE public.stock_transactions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    seq bigint NOT NULL,
    garage_id uuid NOT NULL,
    part_id uuid NOT NULL,
    type public.stock_transaction_type NOT NULL,
    quantity numeric(14,3) NOT NULL,
    unit_cost numeric(18,4),
    unit_id uuid,
    entered_quantity numeric(14,3),
    ref_type public.stock_transaction_ref_type NOT NULL,
    ref_id uuid,
    reason text DEFAULT ''::text NOT NULL,
    supplier_id uuid,
    quantity_after numeric(14,3) DEFAULT 0 NOT NULL,
    average_cost_after numeric(18,4) DEFAULT 0 NOT NULL,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.stock_transactions FORCE ROW LEVEL SECURITY;

ALTER TABLE public.stock_transactions ALTER COLUMN seq ADD GENERATED ALWAYS AS IDENTITY (
    SEQUENCE NAME public.stock_transactions_seq_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);

ALTER TABLE ONLY public.stock_transactions
    ADD CONSTRAINT stock_transactions_pkey PRIMARY KEY (id);

CREATE INDEX idx_stock_transactions_garage_part_created_at ON public.stock_transactions USING btree (garage_id, part_id, created_at DESC);

CREATE INDEX idx_stock_transactions_garage_part_seq ON public.stock_transactions USING btree (garage_id, part_id, seq DESC);

CREATE INDEX idx_stock_transactions_garage_type_created_at ON public.stock_transactions USING btree (garage_id, type, created_at DESC);

CREATE UNIQUE INDEX idx_stock_transactions_stock_issue_part_unique ON public.stock_transactions USING btree (garage_id, part_id, ref_id) WHERE (ref_type = 'stock_issue'::public.stock_transaction_ref_type);

CREATE INDEX idx_stock_transactions_supplier ON public.stock_transactions USING btree (garage_id, supplier_id) WHERE (supplier_id IS NOT NULL);

ALTER TABLE ONLY public.stock_transactions
    ADD CONSTRAINT stock_transactions_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id);

ALTER TABLE ONLY public.stock_transactions
    ADD CONSTRAINT stock_transactions_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id) REFERENCES public.suppliers(garage_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.stock_transactions
    ADD CONSTRAINT stock_transactions_garage_unit_fkey FOREIGN KEY (garage_id, unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

CREATE TRIGGER trigger_stock_transactions_created_audit BEFORE INSERT ON public.stock_transactions FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.stock_transactions ENABLE ROW LEVEL SECURITY;

CREATE POLICY stock_transactions_rls_policy ON public.stock_transactions USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY stock_transactions_rls_policy ON public.stock_transactions IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
