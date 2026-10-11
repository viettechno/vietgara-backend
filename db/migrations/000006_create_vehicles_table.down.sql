-- Drop Row-Level Security policy
DROP POLICY IF EXISTS vehicles_rls_policy ON vehicles;

-- Drop table (its indexes go with it)
DROP TABLE IF EXISTS vehicles;
