-- Drop Row-Level Security policy
DROP POLICY IF EXISTS vehicles_rls_policy ON vehicles;

-- Drop trigger and function
DROP TRIGGER IF EXISTS trigger_vehicles_updated_at ON vehicles;
DROP FUNCTION IF EXISTS update_vehicles_updated_at();

-- Drop table (its indexes go with it)
DROP TABLE IF EXISTS vehicles;
