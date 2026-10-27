-- vietgara_app: the role the backend connects as (via a LOGIN user that is
-- a member of it). It is neither superuser nor BYPASSRLS, so every FORCE
-- ROW LEVEL SECURITY policy applies to it (NFR-SE-03). Tables and sequences
-- created later are granted to it by default; append-only tables revoke
-- UPDATE and DELETE where they are created.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'vietgara_app') THEN
        CREATE ROLE vietgara_app NOLOGIN NOSUPERUSER NOBYPASSRLS;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO vietgara_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO vietgara_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO vietgara_app;
