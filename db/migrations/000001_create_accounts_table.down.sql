-- Drop trigger and function
DROP TRIGGER IF EXISTS trigger_accounts_updated_at ON accounts;
DROP FUNCTION IF EXISTS update_accounts_updated_at();

-- Drop indexes
DROP INDEX IF EXISTS idx_accounts_auth_provider;
DROP INDEX IF EXISTS idx_accounts_identity_platform_uid;
DROP INDEX IF EXISTS idx_accounts_phone;
DROP INDEX IF EXISTS idx_accounts_email;

-- Drop table
DROP TABLE IF EXISTS accounts;

-- Drop enum types
DROP TYPE IF EXISTS account_status;
DROP TYPE IF EXISTS account_auth_provider;
