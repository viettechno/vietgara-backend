-- Drop Row-Level Security policy
DROP POLICY IF EXISTS customers_rls_policy ON customers;

-- Drop table (its indexes go with it). The pg_trgm extension is kept
-- because other tables may rely on it.
DROP TABLE IF EXISTS customers;
