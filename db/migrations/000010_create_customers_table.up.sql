-- Create customers table
-- phone is stored normalized to E.164 ("+84901234567", pkg/phone) by the
-- application so the per-garage uniqueness check below is meaningful and
-- a customer's phone matches a user's verified phone (FR-CUS-06).
-- consent_log is an append-only JSON array of personal-data consent
-- decisions (Decree 13/2023/ND-CP): [{"granted", "recorded_at", "recorded_by"}].
-- A customer that quotes or repair orders refer to is not deleted but
-- deactivated (is_active = false, FR-CAT-07): it leaves pickers and staff
-- can reactivate it. Only an unreferenced customer is soft-deleted
-- (deleted_at).

CREATE TABLE public.customers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    full_name character varying(255) NOT NULL,
    phone character varying(20),
    email character varying(255),
    address text,
    consent_log jsonb DEFAULT '[]'::jsonb NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);

ALTER TABLE ONLY public.customers FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.customers
    ADD CONSTRAINT customers_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.customers
    ADD CONSTRAINT customers_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_customers_email_lower ON public.customers USING btree (lower((email)::text)) WHERE ((deleted_at IS NULL) AND (email IS NOT NULL));

CREATE INDEX idx_customers_email_trgm ON public.customers USING gin (email public.gin_trgm_ops);

CREATE INDEX idx_customers_full_name_trgm ON public.customers USING gin (full_name public.gin_trgm_ops);

CREATE INDEX idx_customers_garage_created_at ON public.customers USING btree (garage_id, created_at DESC) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX idx_customers_garage_phone_unique ON public.customers USING btree (garage_id, phone) WHERE ((deleted_at IS NULL) AND (phone IS NOT NULL));

CREATE INDEX idx_customers_phone_trgm ON public.customers USING gin (phone public.gin_trgm_ops);

ALTER TABLE ONLY public.customers
    ADD CONSTRAINT customers_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_customers_row_audit BEFORE INSERT OR UPDATE ON public.customers FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.customers ENABLE ROW LEVEL SECURITY;

CREATE POLICY customers_rls_policy ON public.customers USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY customers_rls_policy ON public.customers IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
