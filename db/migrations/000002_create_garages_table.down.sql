DROP TRIGGER IF EXISTS trigger_garages_updated_at ON garages;
DROP FUNCTION IF EXISTS update_garages_updated_at();

DROP TABLE IF EXISTS garages;

DROP TYPE IF EXISTS garage_status;
DROP TYPE IF EXISTS quote_approval_mode;

DROP FUNCTION IF EXISTS app_current_user_id();
DROP FUNCTION IF EXISTS app_current_garage_id();
