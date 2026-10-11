-- Drop Row-Level Security policy
DROP POLICY IF EXISTS suppliers_rls_policy ON suppliers;

-- Drop table (its indexes go with it)
DROP TABLE IF EXISTS suppliers;
