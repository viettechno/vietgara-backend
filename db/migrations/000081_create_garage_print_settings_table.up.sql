-- Document/Print (Release 10.0, FR-DOC-04): the paper size and the copy mark a
-- garage chose for each printable document type. A garage that never changed
-- a type has no row and prints A4 without the copy mark.

CREATE TABLE public.garage_print_settings (
    garage_id uuid NOT NULL,
    document_type character varying(20) NOT NULL,
    paper_size character varying(2) DEFAULT 'A4'::character varying NOT NULL,
    copy_mark boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid,
    CONSTRAINT garage_print_settings_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['quote'::character varying, 'repair_order'::character varying, 'settlement'::character varying, 'payment_voucher'::character varying, 'goods_receipt'::character varying, 'stock_issue'::character varying])::text[]))),
    CONSTRAINT garage_print_settings_paper_size_check CHECK (((paper_size)::text = ANY ((ARRAY['A4'::character varying, 'A5'::character varying])::text[])))
);

ALTER TABLE ONLY public.garage_print_settings FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.garage_print_settings
    ADD CONSTRAINT garage_print_settings_pkey PRIMARY KEY (garage_id, document_type);

ALTER TABLE ONLY public.garage_print_settings
    ADD CONSTRAINT garage_print_settings_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_garage_print_settings_row_audit BEFORE INSERT OR UPDATE ON public.garage_print_settings FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.garage_print_settings ENABLE ROW LEVEL SECURITY;

CREATE POLICY garage_print_settings_rls_policy ON public.garage_print_settings USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY garage_print_settings_rls_policy ON public.garage_print_settings IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
