ALTER TABLE stock_issues ALTER COLUMN created_at SET DEFAULT NOW();
ALTER TABLE stock_issues DROP COLUMN IF EXISTS is_short;
ALTER TABLE repair_orders DROP COLUMN IF EXISTS waiting_on_parts;
-- The 'cancelled' value of repair_order_status stays: Postgres cannot drop an
-- enum value, and an unused value is harmless.
DROP INDEX IF EXISTS idx_quote_lines_job;
ALTER TABLE repair_order_lines DROP COLUMN IF EXISTS non_billable, DROP COLUMN IF EXISTS job_id;
ALTER TABLE quote_lines DROP COLUMN IF EXISTS non_billable, DROP COLUMN IF EXISTS job_id;
DROP TABLE IF EXISTS job_parts;
DROP TABLE IF EXISTS jobs;
