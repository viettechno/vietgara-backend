-- Create suppliers table
-- contact_info holds {"contact_person", "phone", "email", "address"};
-- bank_details holds {"bank_name", "account_number", "account_holder"}.
-- A supplier that documents refer to is deactivated (is_active = false,
-- FR-CAT-07), not deleted, so those documents keep working; only an
-- unreferenced supplier is soft-deleted (deleted_at).

CREATE TABLE public.suppliers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    tax_code character varying(20),
    contact_info jsonb DEFAULT '{}'::jsonb NOT NULL,
    bank_details jsonb DEFAULT '{}'::jsonb NOT NULL,
    note text,
    is_active boolean DEFAULT true NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);

ALTER TABLE ONLY public.suppliers FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.suppliers
    ADD CONSTRAINT suppliers_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.suppliers
    ADD CONSTRAINT suppliers_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_suppliers_garage ON public.suppliers USING btree (garage_id) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX idx_suppliers_garage_name_unique ON public.suppliers USING btree (garage_id, lower((name)::text)) WHERE (deleted_at IS NULL);

ALTER TABLE ONLY public.suppliers
    ADD CONSTRAINT suppliers_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_suppliers_row_audit BEFORE INSERT OR UPDATE ON public.suppliers FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.suppliers ENABLE ROW LEVEL SECURITY;

CREATE POLICY suppliers_rls_policy ON public.suppliers USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY suppliers_rls_policy ON public.suppliers IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
