-- The platform audit log: entries that belong to no garage (a plan, platform
-- staff or platform payment settings changed, a platform sign-in) can be read
-- when the request sets app.platform_audit, which only the audit.read query
-- does, inside its own transaction. Entries of a garage stay readable within
-- that garage only, and the log stays append-only.
CREATE POLICY audit_log_select_platform ON public.audit_log FOR SELECT
    USING (((garage_id IS NULL) AND (current_setting('app.platform_audit'::text, true) = 'on'::text)));
