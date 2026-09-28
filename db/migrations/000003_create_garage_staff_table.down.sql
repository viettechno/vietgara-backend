DROP TRIGGER IF EXISTS trigger_garage_staff_updated_at ON garage_staff;
DROP FUNCTION IF EXISTS update_garage_staff_updated_at();

DROP TABLE IF EXISTS garage_staff;

DROP TYPE IF EXISTS staff_status;
