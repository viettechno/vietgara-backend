-- Drop Row-Level Security policy
DROP POLICY IF EXISTS suppliers_rls_policy ON suppliers;

-- Drop trigger and function
DROP TRIGGER IF EXISTS trigger_suppliers_updated_at ON suppliers;
DROP FUNCTION IF EXISTS update_suppliers_updated_at();

-- Drop table (its indexes go with it)
DROP TABLE IF EXISTS suppliers;
