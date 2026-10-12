DROP TRIGGER IF EXISTS trigger_platform_payment_settings_row_audit ON platform_payment_settings;
DROP TABLE IF EXISTS platform_payment_settings;

DROP TRIGGER IF EXISTS trigger_credit_ledger_created_audit ON credit_ledger;
DROP TABLE IF EXISTS credit_ledger;

DROP TRIGGER IF EXISTS trigger_plan_payment_requests_row_audit ON plan_payment_requests;
DROP TABLE IF EXISTS plan_payment_requests;

DROP TYPE IF EXISTS credit_entry_kind;
DROP TYPE IF EXISTS plan_payment_status;
DROP TYPE IF EXISTS plan_payment_kind;
