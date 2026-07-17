-- Disable Row-Level Security
ALTER TABLE garage_staff DISABLE ROW LEVEL SECURITY;

-- Drop Row-Level Security policy
DROP POLICY IF EXISTS garage_staff_rls_policy ON garage_staff;

-- Drop trigger and function
DROP TRIGGER IF EXISTS trigger_garage_staff_updated_at ON garage_staff;
DROP FUNCTION IF EXISTS update_garage_staff_updated_at();

-- Drop indexes (unique index is also dropped)
DROP INDEX IF EXISTS idx_garage_staff_garage_account_unique;
DROP INDEX IF EXISTS idx_garage_staff_status;
DROP INDEX IF EXISTS idx_garage_staff_role;
DROP INDEX IF EXISTS idx_garage_staff_account_id;
DROP INDEX IF EXISTS idx_garage_staff_garage_id;

-- Drop table
DROP TABLE IF EXISTS garage_staff;

-- Drop enum types
DROP TYPE IF EXISTS staff_status;
DROP TYPE IF EXISTS staff_role;
