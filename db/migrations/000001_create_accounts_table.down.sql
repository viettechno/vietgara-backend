DROP TRIGGER IF EXISTS trigger_accounts_updated_at ON accounts;
DROP FUNCTION IF EXISTS update_accounts_updated_at();

DROP TABLE IF EXISTS accounts;

DROP TYPE IF EXISTS account_locale;
DROP TYPE IF EXISTS account_auth_provider;
DROP TYPE IF EXISTS account_status;
