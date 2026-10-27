-- Create vehicles table
-- license_plate keeps the display form ("51F-123.45"); license_plate_key is
-- the same plate reduced to upper-case letters and digits ("51F12345") and
-- is what uniqueness and plate search use, so formatting differences never
-- create duplicates. A vehicle that quotes or repair orders refer to is
-- deactivated (is_active = false, FR-CAT-07), not deleted, so its history
-- (FR-CUS-03) stays; only an unreferenced vehicle is soft-deleted
-- (deleted_at).

CREATE TABLE public.vehicles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    license_plate character varying(20) NOT NULL,
    license_plate_key character varying(20) GENERATED ALWAYS AS (upper(regexp_replace((license_plate)::text, '[^A-Za-z0-9]'::text, ''::text, 'g'::text))) STORED,
    make character varying(100),
    model character varying(100),
    year integer,
    vin character varying(50),
    engine_number character varying(50),
    is_active boolean DEFAULT true NOT NULL,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT vehicles_year_check CHECK (((year >= 1886) AND (year <= 2100)))
);

ALTER TABLE ONLY public.vehicles FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.vehicles
    ADD CONSTRAINT vehicles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.vehicles
    ADD CONSTRAINT vehicles_garage_id_id_key UNIQUE (garage_id, id);

CREATE INDEX idx_vehicles_customer_id ON public.vehicles USING btree (customer_id) WHERE (deleted_at IS NULL);

CREATE INDEX idx_vehicles_garage_created_at ON public.vehicles USING btree (garage_id, created_at DESC) WHERE (deleted_at IS NULL);

CREATE INDEX idx_vehicles_garage_license_plate ON public.vehicles USING btree (garage_id, license_plate);

CREATE UNIQUE INDEX idx_vehicles_garage_plate_unique ON public.vehicles USING btree (garage_id, license_plate_key) WHERE (deleted_at IS NULL);

CREATE INDEX idx_vehicles_license_plate_key_trgm ON public.vehicles USING gin (license_plate_key public.gin_trgm_ops);

ALTER TABLE ONLY public.vehicles
    ADD CONSTRAINT vehicles_garage_customer_fkey FOREIGN KEY (garage_id, customer_id) REFERENCES public.customers(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.vehicles
    ADD CONSTRAINT vehicles_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_vehicles_row_audit BEFORE INSERT OR UPDATE ON public.vehicles FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.vehicles ENABLE ROW LEVEL SECURITY;

CREATE POLICY vehicles_rls_policy ON public.vehicles USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY vehicles_rls_policy ON public.vehicles IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
