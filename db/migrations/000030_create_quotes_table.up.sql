-- A quote's public_token_hash/public_token_expires_at back the customer
-- approval link (FR-QUO-02, QUOTE_APPROVAL_MODE_LINK): the plaintext token
-- is returned once by SendQuote and never stored; only its SHA-256 hash
-- is, so a leaked database dump cannot be used to forge a link. Cleared
-- once the quote is decided or the link expires. Rows are soft-deleted
-- (deleted_at) because vehicle history (FR-CUS-03) and, later, repair
-- orders keep referencing them.

CREATE TYPE public.quote_status AS ENUM (
    'draft',
    'sent',
    'approved',
    'rejected',
    'converted'
);

CREATE TABLE public.quotes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    vehicle_id uuid NOT NULL,
    customer_name character varying(255) NOT NULL,
    vehicle_license_plate character varying(20) NOT NULL,
    number character varying(50) NOT NULL,
    status public.quote_status DEFAULT 'draft'::public.quote_status NOT NULL,
    approval_mode public.quote_approval_mode DEFAULT 'link'::public.quote_approval_mode NOT NULL,
    notes text,
    total_amount bigint DEFAULT 0 NOT NULL,
    issuer jsonb DEFAULT '{}'::jsonb NOT NULL,
    billing_party jsonb DEFAULT '{}'::jsonb NOT NULL,
    public_token_hash character(64),
    public_token_expires_at timestamp with time zone,
    sent_at timestamp with time zone,
    decided_at timestamp with time zone,
    converted_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);

ALTER TABLE ONLY public.quotes FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.quotes
    ADD CONSTRAINT quotes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.quotes
    ADD CONSTRAINT quotes_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_quotes_billing_profile ON public.quotes USING btree (garage_id, ((billing_party ->> 'tax_profile_id'::text))) WHERE ((deleted_at IS NULL) AND ((billing_party ->> 'tax_profile_id'::text) IS NOT NULL));

CREATE INDEX idx_quotes_customer_id ON public.quotes USING btree (garage_id, customer_id, created_at DESC) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX idx_quotes_garage_number_unique ON public.quotes USING btree (garage_id, number) WHERE (deleted_at IS NULL);

CREATE INDEX idx_quotes_garage_status_created_at ON public.quotes USING btree (garage_id, status, created_at DESC) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX idx_quotes_public_token_hash ON public.quotes USING btree (public_token_hash) WHERE (public_token_hash IS NOT NULL);

CREATE INDEX idx_quotes_vehicle_id ON public.quotes USING btree (garage_id, vehicle_id, created_at DESC) WHERE (deleted_at IS NULL);

ALTER TABLE ONLY public.quotes
    ADD CONSTRAINT quotes_garage_customer_fkey FOREIGN KEY (garage_id, customer_id) REFERENCES public.customers(garage_id, id);

ALTER TABLE ONLY public.quotes
    ADD CONSTRAINT quotes_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.quotes
    ADD CONSTRAINT quotes_garage_vehicle_fkey FOREIGN KEY (garage_id, vehicle_id) REFERENCES public.vehicles(garage_id, id);

CREATE TRIGGER trigger_quotes_row_audit BEFORE INSERT OR UPDATE ON public.quotes FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.quotes ENABLE ROW LEVEL SECURITY;

CREATE POLICY quotes_rls_policy ON public.quotes USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY quotes_rls_policy ON public.quotes IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
