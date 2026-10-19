-- Release 7.2: part barcodes (FR-INV-13) and the Excel import history
-- (FR-INV-14). part_barcodes is owned by Catalogue; import_runs by the
-- Data Import module. Both point at garages and parts only through
-- composite keys so every row stays in one garage.

-- ---------------------------------------------------------------------------
-- part_barcodes: the scannable codes of a part. A code belongs to the
-- garage (not just the part): it is unique across every part of the
-- garage, case-insensitively, and is matched the same way by lookup and
-- search. unit_id is NULL for the base unit and otherwise points at one
-- of the part's packaging rows (a case barcode).
-- ---------------------------------------------------------------------------
CREATE TABLE part_barcodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    part_id UUID NOT NULL,
    unit_id UUID,
    -- Stored as typed after trimming (1..64 characters, see the domain).
    barcode VARCHAR(64) NOT NULL,
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT part_barcodes_garage_id_id_key UNIQUE (garage_id, id),
    CONSTRAINT part_barcodes_garage_part_fkey FOREIGN KEY (garage_id, part_id)
        REFERENCES parts (garage_id, id) ON DELETE CASCADE,
    -- A code may only name a packaging unit of its own part. The composite
    -- key also keeps the unit in the same garage as the part.
    CONSTRAINT part_barcodes_garage_part_unit_fkey FOREIGN KEY (garage_id, part_id, unit_id)
        REFERENCES part_units (garage_id, part_id, unit_id) ON DELETE RESTRICT
);

-- One code per garage, regardless of case. Soft-deleting a part deletes
-- its barcodes in the same transaction, so its codes are free again.
CREATE UNIQUE INDEX idx_part_barcodes_garage_barcode_unique ON part_barcodes (garage_id, lower(barcode));
CREATE INDEX idx_part_barcodes_part ON part_barcodes (garage_id, part_id);

ALTER TABLE part_barcodes ENABLE ROW LEVEL SECURITY;
ALTER TABLE part_barcodes FORCE ROW LEVEL SECURITY;

CREATE POLICY part_barcodes_rls_policy ON part_barcodes
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY part_barcodes_rls_policy ON part_barcodes IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- Append-only: created_by comes from the session user (NFR-MT-07).
CREATE TRIGGER trigger_part_barcodes_created_audit BEFORE INSERT ON part_barcodes
FOR EACH ROW EXECUTE FUNCTION set_created_audit();

-- ---------------------------------------------------------------------------
-- import_runs: one row per confirmed Excel import (FR-INV-14). Its unique
-- (garage, type, file hash) key is the idempotency guard: confirming the
-- same content twice cannot create a second document. It doubles as the
-- import history and keeps the document the import created.
-- ---------------------------------------------------------------------------
CREATE TABLE import_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    import_type VARCHAR(32) NOT NULL CHECK (import_type IN ('parts', 'goods_receipts', 'stock_counts')),
    -- SHA-256 of the uploaded file, lower-case hex.
    file_sha256 CHAR(64) NOT NULL,
    file_name VARCHAR(255) NOT NULL DEFAULT '',
    row_count INTEGER NOT NULL DEFAULT 0 CHECK (row_count >= 0),
    -- The completed receipt, draft receipt or draft count the import made.
    document_id UUID,
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT import_runs_garage_id_id_key UNIQUE (garage_id, id),
    CONSTRAINT import_runs_garage_type_file_key UNIQUE (garage_id, import_type, file_sha256)
);

CREATE INDEX idx_import_runs_garage_created_at ON import_runs (garage_id, created_at DESC);

ALTER TABLE import_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE import_runs FORCE ROW LEVEL SECURITY;

CREATE POLICY import_runs_rls_policy ON import_runs
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY import_runs_rls_policy ON import_runs IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

CREATE TRIGGER trigger_import_runs_created_audit BEFORE INSERT ON import_runs
FOR EACH ROW EXECUTE FUNCTION set_created_audit();
