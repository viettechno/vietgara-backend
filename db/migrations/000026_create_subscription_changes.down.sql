DROP TRIGGER IF EXISTS trigger_subscription_changes_row_audit ON subscription_changes;
DROP TABLE IF EXISTS subscription_changes;

DROP TYPE IF EXISTS plan_change_status;
