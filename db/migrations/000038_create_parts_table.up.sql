-- parts: the part (FR-INV-01, FR-CAT-01..04/09). Soft-deleted (deleted_at)
-- because stock_transactions and document lines keep referencing them. Stock
-- is always kept in base_unit_id; the quantities live in Inventory.

CREATE TABLE public.parts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    sku character varying(50) NOT NULL,
    name character varying(255) NOT NULL,
    base_unit_id uuid NOT NULL,
    sale_price bigint DEFAULT 0 NOT NULL,
    category_id uuid,
    brand character varying(100) DEFAULT ''::character varying NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    reorder_threshold numeric(14,3) DEFAULT 0 NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT parts_sale_price_check CHECK ((sale_price >= 0))
);

ALTER TABLE ONLY public.parts FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.parts
    ADD CONSTRAINT parts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.parts
    ADD CONSTRAINT parts_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_parts_brand_trgm ON public.parts USING gin (brand public.gin_trgm_ops);

CREATE INDEX idx_parts_garage_category ON public.parts USING btree (garage_id, category_id) WHERE (deleted_at IS NULL);

CREATE INDEX idx_parts_garage_created_at ON public.parts USING btree (garage_id, created_at DESC) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX idx_parts_garage_sku_unique ON public.parts USING btree (garage_id, sku) WHERE (deleted_at IS NULL);

CREATE INDEX idx_parts_name_trgm ON public.parts USING gin (name public.gin_trgm_ops);

CREATE INDEX idx_parts_sku_trgm ON public.parts USING gin (sku public.gin_trgm_ops);

ALTER TABLE ONLY public.parts
    ADD CONSTRAINT parts_garage_base_unit_fkey FOREIGN KEY (garage_id, base_unit_id) REFERENCES public.units(garage_id, id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.parts
    ADD CONSTRAINT parts_garage_category_fkey FOREIGN KEY (garage_id, category_id) REFERENCES public.part_categories(garage_id, id) ON DELETE SET NULL (category_id);

ALTER TABLE ONLY public.parts
    ADD CONSTRAINT parts_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_parts_row_audit BEFORE INSERT OR UPDATE ON public.parts FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.parts ENABLE ROW LEVEL SECURITY;

CREATE POLICY parts_rls_policy ON public.parts USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY parts_rls_policy ON public.parts IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
