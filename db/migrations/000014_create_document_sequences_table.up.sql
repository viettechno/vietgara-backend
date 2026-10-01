-- Platform infrastructure (like outbox_events): a per-garage, per-document-
-- type, per-period counter used to render numbers from a garage's
-- numbering templates (FR-TEN-05, e.g. "BG-{YYYY}{MM}-{SEQ:4}"). Quote &
-- Pricing (Release 1.2) and, later, Repair Order (Release 1.3) each
-- reserve their next sequence atomically; not owned by either module.
--
-- period is the template's date placeholders rendered for "now" (e.g.
-- "202609" for a template using {YYYY}{MM}), so the counter resets exactly
-- when the template's own date part changes -- daily, monthly or yearly,
-- whatever the garage configured.
CREATE TABLE document_sequences (
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    -- 'quote' today; 'repair_order' from Release 1.3. Free text (not an
    -- enum) so a later release adds a document type without a migration
    -- here.
    doc_type VARCHAR(30) NOT NULL,
    period VARCHAR(20) NOT NULL,
    next_seq INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (garage_id, doc_type, period)
);

ALTER TABLE document_sequences ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_sequences FORCE ROW LEVEL SECURITY;

CREATE POLICY document_sequences_rls_policy ON document_sequences
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY document_sequences_rls_policy ON document_sequences IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
