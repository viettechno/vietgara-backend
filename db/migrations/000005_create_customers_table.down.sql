-- Drop Row-Level Security policy
DROP POLICY IF EXISTS customers_rls_policy ON customers;

-- Drop trigger and function
DROP TRIGGER IF EXISTS trigger_customers_updated_at ON customers;
DROP FUNCTION IF EXISTS update_customers_updated_at();

-- Drop table (its indexes go with it). The pg_trgm extension is kept
-- because other tables may rely on it.
DROP TABLE IF EXISTS customers;
