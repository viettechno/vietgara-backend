DROP TRIGGER IF EXISTS trigger_users_updated_at ON users;
DROP FUNCTION IF EXISTS update_users_updated_at();

DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS user_locale;
DROP TYPE IF EXISTS user_auth_provider;
DROP TYPE IF EXISTS user_status;
