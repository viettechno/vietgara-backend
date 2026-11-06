ALTER TABLE public.garage_print_settings NO FORCE ROW LEVEL SECURITY;
DELETE FROM public.garage_print_settings WHERE document_type IN ('payment_out', 'purchase_order');
ALTER TABLE public.garage_print_settings FORCE ROW LEVEL SECURITY;

ALTER TABLE public.garage_print_settings DROP CONSTRAINT garage_print_settings_document_type_check;

ALTER TABLE public.garage_print_settings
    ADD CONSTRAINT garage_print_settings_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['quote'::character varying, 'repair_order'::character varying, 'settlement'::character varying, 'payment_voucher'::character varying, 'goods_receipt'::character varying, 'stock_issue'::character varying])::text[]))) NOT VALID;

ALTER TABLE public.garage_print_settings VALIDATE CONSTRAINT garage_print_settings_document_type_check;

ALTER TABLE public.print_log NO FORCE ROW LEVEL SECURITY;
DELETE FROM public.print_log WHERE document_type IN ('payment_out', 'purchase_order');
ALTER TABLE public.print_log FORCE ROW LEVEL SECURITY;

ALTER TABLE public.print_log DROP CONSTRAINT print_log_document_type_check;

ALTER TABLE public.print_log
    ADD CONSTRAINT print_log_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['quote'::character varying, 'repair_order'::character varying, 'settlement'::character varying, 'payment_voucher'::character varying, 'goods_receipt'::character varying, 'stock_issue'::character varying])::text[]))) NOT VALID;

ALTER TABLE public.print_log VALIDATE CONSTRAINT print_log_document_type_check;
