-- Drop Row-Level Security policy
DROP POLICY IF EXISTS partners_rls_policy ON partners;

-- Drop trigger and function
DROP TRIGGER IF EXISTS trigger_partners_updated_at ON partners;
DROP FUNCTION IF EXISTS update_partners_updated_at();

-- Drop table (its indexes go with it)
DROP TABLE IF EXISTS partners;

-- Drop enum type
DROP TYPE IF EXISTS partner_type;
