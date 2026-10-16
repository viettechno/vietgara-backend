-- File registry (Release 5.0): every uploaded file is a row here, owned by a
-- garage or by a user, instead of a URL in a business column. Business rows
-- keep the object key (garages.logo_key), never the URL.
--
-- Key layout (also the object key in storage):
--   garages/{garage_id}/{kind}/{entity_id}/{uuid}.{ext}
--   users/{user_id}/{kind}/{uuid}.{ext}
-- Public kinds (the logo) are served by URL; every later kind is private and
-- served through short-lived signed URLs.
--
-- A file that a business row refers to is not deleted but deactivated
-- (is_active = false, FR-CAT-07); an unreferenced one is soft-deleted
-- (deleted_at) and its object removed.

CREATE TYPE file_kind AS ENUM ('logo', 'payment_evidence', 'platform_qr', 'part_image');

CREATE TABLE files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Exactly one owner: a garage's file or a user's own file.
    garage_id UUID REFERENCES garages(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    kind file_kind NOT NULL,
    -- What the file belongs to, e.g. 'garage' and the garage's id.
    entity_type VARCHAR(50) NOT NULL,
    entity_id UUID NOT NULL,
    key VARCHAR(500) NOT NULL UNIQUE,
    content_type VARCHAR(100) NOT NULL,
    size BIGINT NOT NULL CHECK (size >= 0),
    -- Hex SHA-256 of the content.
    sha256 CHAR(64) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT files_one_owner CHECK ((garage_id IS NOT NULL) <> (user_id IS NOT NULL))
);

CREATE INDEX idx_files_garage_entity ON files (garage_id, entity_type, entity_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_files_user ON files (user_id, kind) WHERE deleted_at IS NULL AND user_id IS NOT NULL;

CREATE TRIGGER trigger_files_row_audit BEFORE INSERT OR UPDATE ON files FOR EACH ROW EXECUTE FUNCTION set_row_audit();

-- Same row-level security as other garage data; a user's own files are
-- visible to that user only.
ALTER TABLE files ENABLE ROW LEVEL SECURITY;
ALTER TABLE files FORCE ROW LEVEL SECURITY;

CREATE POLICY files_rls_policy ON files
FOR ALL
USING (
    (garage_id IS NOT NULL AND garage_id = app_current_garage_id())
    OR (user_id IS NOT NULL AND user_id = app_current_user_id())
)
WITH CHECK (
    (garage_id IS NOT NULL AND garage_id = app_current_garage_id())
    OR (user_id IS NOT NULL AND user_id = app_current_user_id())
);

COMMENT ON POLICY files_rls_policy ON files IS
'A garage file is visible in its garage (app.current_garage_id); a user file to its user (app.current_user_id).';

-- The License module serves a subscription payment's evidence and the
-- platform's QR code to platform admins, who are not the file's owner: this
-- function runs SECURITY DEFINER and returns only the object key (and its
-- content type) of a live file by its id, never a row. Callers authorize
-- the read themselves.
CREATE OR REPLACE FUNCTION file_object_key(file_id UUID)
RETURNS TABLE (key VARCHAR, content_type VARCHAR)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
    SELECT f.key, f.content_type FROM files f WHERE f.id = file_id AND f.deleted_at IS NULL
$$;

GRANT EXECUTE ON FUNCTION file_object_key(UUID) TO vietgara_app;
