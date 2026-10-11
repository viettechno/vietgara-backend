DROP POLICY IF EXISTS files_rls_policy ON files;
DROP TRIGGER IF EXISTS trigger_files_row_audit ON files;
DROP TABLE IF EXISTS files;

DROP TYPE IF EXISTS file_kind;
