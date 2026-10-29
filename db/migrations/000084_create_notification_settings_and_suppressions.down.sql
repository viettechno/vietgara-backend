DROP POLICY IF EXISTS notification_log_rls_policy ON public.notification_log;

ALTER TABLE public.notification_log NO FORCE ROW LEVEL SECURITY;
ALTER TABLE public.notification_log DISABLE ROW LEVEL SECURITY;

DROP INDEX IF EXISTS public.idx_notification_log_garage;

ALTER TABLE public.notification_log
    DROP CONSTRAINT IF EXISTS notification_log_garage_id_fkey,
    DROP CONSTRAINT IF EXISTS notification_log_skip_reason_check,
    DROP COLUMN IF EXISTS skip_reason,
    DROP COLUMN IF EXISTS event_id,
    DROP COLUMN IF EXISTS entity_id,
    DROP COLUMN IF EXISTS entity_type,
    DROP COLUMN IF EXISTS type,
    DROP COLUMN IF EXISTS customer_id,
    DROP COLUMN IF EXISTS garage_id;

-- The 'skipped' value of notification_status stays: Postgres cannot drop an
-- enum value, and nothing uses it once the columns above are gone.

DROP TABLE IF EXISTS public.email_suppressions;
DROP TABLE IF EXISTS public.garage_notification_settings;
