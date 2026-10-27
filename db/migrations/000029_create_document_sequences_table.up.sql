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

CREATE TABLE public.document_sequences (
    garage_id uuid NOT NULL,
    doc_type character varying(30) NOT NULL,
    period character varying(20) NOT NULL,
    next_seq integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid
);

ALTER TABLE ONLY public.document_sequences FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.document_sequences
    ADD CONSTRAINT document_sequences_pkey PRIMARY KEY (garage_id, doc_type, period);

ALTER TABLE ONLY public.document_sequences
    ADD CONSTRAINT document_sequences_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_document_sequences_row_audit BEFORE INSERT OR UPDATE ON public.document_sequences FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.document_sequences ENABLE ROW LEVEL SECURITY;

CREATE POLICY document_sequences_rls_policy ON public.document_sequences USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY document_sequences_rls_policy ON public.document_sequences IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
