-- Document/Print (Release 10.0, FR-DOC-04, FR-DOC-05): one row for every
-- print of a document: who (created_by), when (created_at), on which paper
-- and with which version of the template. The earliest row of a document
-- pins the template version of its reprints; the number of rows says whether
-- a print is a reprint. Append-only for the application role.

CREATE TABLE public.print_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    document_type character varying(20) NOT NULL,
    document_id uuid NOT NULL,
    paper_size character varying(2) NOT NULL,
    template_version integer NOT NULL,
    copy boolean NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    CONSTRAINT print_log_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['quote'::character varying, 'repair_order'::character varying, 'settlement'::character varying, 'payment_voucher'::character varying, 'goods_receipt'::character varying, 'stock_issue'::character varying])::text[]))),
    CONSTRAINT print_log_paper_size_check CHECK (((paper_size)::text = ANY ((ARRAY['A4'::character varying, 'A5'::character varying])::text[]))),
    CONSTRAINT print_log_template_version_check CHECK ((template_version > 0))
);

ALTER TABLE ONLY public.print_log FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.print_log
    ADD CONSTRAINT print_log_pkey PRIMARY KEY (id);

CREATE INDEX idx_print_log_document ON public.print_log USING btree (garage_id, document_type, document_id, paper_size, created_at);

ALTER TABLE ONLY public.print_log
    ADD CONSTRAINT print_log_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_print_log_created_audit BEFORE INSERT ON public.print_log FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

ALTER TABLE public.print_log ENABLE ROW LEVEL SECURITY;

CREATE POLICY print_log_rls_policy ON public.print_log USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY print_log_rls_policy ON public.print_log IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

REVOKE UPDATE, DELETE ON public.print_log FROM vietgara_app;
