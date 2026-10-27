CREATE TYPE public.customer_tax_profile_kind AS ENUM (
    'personal',
    'company'
);

CREATE TABLE public.customer_tax_profiles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    kind public.customer_tax_profile_kind NOT NULL,
    legal_name character varying(255) NOT NULL,
    tax_id character varying(13),
    address character varying(500),
    invoice_email character varying(255),
    is_default boolean DEFAULT false NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT customer_tax_profiles_tax_id_check CHECK (((tax_id IS NULL) OR ((tax_id)::text ~ '^[0-9]{10}([0-9]{3})?$'::text)))
);

ALTER TABLE ONLY public.customer_tax_profiles FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.customer_tax_profiles
    ADD CONSTRAINT customer_tax_profiles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.customer_tax_profiles
    ADD CONSTRAINT customer_tax_profiles_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_customer_tax_profiles_customer ON public.customer_tax_profiles USING btree (garage_id, customer_id, created_at) WHERE (deleted_at IS NULL);

CREATE UNIQUE INDEX idx_customer_tax_profiles_default ON public.customer_tax_profiles USING btree (customer_id) WHERE (is_default AND (deleted_at IS NULL));

CREATE UNIQUE INDEX idx_customer_tax_profiles_personal ON public.customer_tax_profiles USING btree (customer_id) WHERE ((kind = 'personal'::public.customer_tax_profile_kind) AND (deleted_at IS NULL));

ALTER TABLE ONLY public.customer_tax_profiles
    ADD CONSTRAINT customer_tax_profiles_customer_fkey FOREIGN KEY (garage_id, customer_id) REFERENCES public.customers(garage_id, id) ON DELETE CASCADE;

CREATE TRIGGER trigger_customer_tax_profiles_row_audit BEFORE INSERT OR UPDATE ON public.customer_tax_profiles FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.customer_tax_profiles ENABLE ROW LEVEL SECURITY;

CREATE POLICY customer_tax_profiles_rls_policy ON public.customer_tax_profiles USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY customer_tax_profiles_rls_policy ON public.customer_tax_profiles IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
