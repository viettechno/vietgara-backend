-- Customer & Vehicle module (HLD module #4), Database Design Section 3.2.

-- Create vehicles table
-- license_plate keeps the display form ("51F-123.45"); license_plate_key is
-- the same plate reduced to upper-case letters and digits ("51F12345") and
-- is what uniqueness and plate search use, so formatting differences never
-- create duplicates. A vehicle that quotes or repair orders refer to is
-- deactivated (is_active = false, FR-CAT-07), not deleted, so its history
-- (FR-CUS-03) stays; only an unreferenced vehicle is soft-deleted
-- (deleted_at).
CREATE TABLE vehicles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    customer_id UUID NOT NULL,
    license_plate VARCHAR(20) NOT NULL,
    license_plate_key VARCHAR(20) GENERATED ALWAYS AS (upper(regexp_replace(license_plate, '[^A-Za-z0-9]', '', 'g'))) STORED,
    make VARCHAR(100),
    model VARCHAR(100),
    year INTEGER CHECK (year BETWEEN 1886 AND 2100),
    vin VARCHAR(50),
    engine_number VARCHAR(50),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    -- A vehicle's owner must belong to the same garage as the vehicle.
    CONSTRAINT vehicles_garage_customer_fkey FOREIGN KEY (garage_id, customer_id)
        REFERENCES customers (garage_id, id) ON DELETE CASCADE,
    -- Target of the quotes/repair_orders (garage_id, vehicle_id) foreign
    -- keys (Release 1.2), which guarantee a quote and its vehicle always
    -- belong to the same garage.
    CONSTRAINT vehicles_garage_id_id_key UNIQUE (garage_id, id)
);

-- Indexes (Database Design Section 8: (garage_id, license_plate))
CREATE INDEX idx_vehicles_garage_license_plate ON vehicles (garage_id, license_plate);
CREATE INDEX idx_vehicles_customer_id ON vehicles (customer_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_vehicles_garage_created_at ON vehicles (garage_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_vehicles_license_plate_key_trgm ON vehicles USING gin (license_plate_key gin_trgm_ops);

-- One live vehicle per plate within a garage
CREATE UNIQUE INDEX idx_vehicles_garage_plate_unique ON vehicles (garage_id, license_plate_key) WHERE deleted_at IS NULL;

-- Enable Row-Level Security
ALTER TABLE vehicles ENABLE ROW LEVEL SECURITY;

-- CRITICAL: Force RLS so table owners/app users cannot bypass it
ALTER TABLE vehicles FORCE ROW LEVEL SECURITY;

-- Create Row-Level Security policy for vehicles
CREATE POLICY vehicles_rls_policy ON vehicles
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY vehicles_rls_policy ON vehicles IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
