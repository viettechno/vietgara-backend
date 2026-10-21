-- audit_log was the one garage-keyed table without row-level security: the
-- application role may only INSERT and SELECT (000013), but nothing stopped a
-- SELECT from reading another garage's entries. The owner's support access
-- log (Release 8.2) will read it, so the isolation is put in place first.
--
--   * Any transaction may append an entry, for any garage or for none (a
--     platform event): the row is written by the code that makes the change.
--   * A SELECT sees the entries of the garage the transaction is scoped to
--     (app_current_garage_id()), nothing else; entries without a garage are
--     not readable through the application role.
--   * There is no UPDATE or DELETE policy, on top of the revoked privileges.

ALTER TABLE audit_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log FORCE ROW LEVEL SECURITY;

CREATE POLICY audit_log_insert ON audit_log FOR INSERT WITH CHECK (true);
CREATE POLICY audit_log_select ON audit_log FOR SELECT
    USING (garage_id IS NOT NULL AND garage_id = app_current_garage_id());
