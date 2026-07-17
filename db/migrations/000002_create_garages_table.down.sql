-- Disable Row-Level Security
ALTER TABLE garages DISABLE ROW LEVEL SECURITY;

-- Drop Row-Level Security policy
DROP POLICY IF EXISTS garages_rls_policy ON garages;

-- Drop trigger and function
DROP TRIGGER IF EXISTS trigger_garages_updated_at ON garages;
DROP FUNCTION IF EXISTS update_garages_updated_at();

-- Drop indexes
DROP INDEX IF EXISTS idx_garages_status;
DROP INDEX IF EXISTS idx_garages_account_id;

-- Drop table
DROP TABLE IF EXISTS garages;

-- Drop enum type
DROP TYPE IF EXISTS garage_status;
