DELETE FROM public.garage_print_settings WHERE document_type IN ('payment_out', 'purchase_order');

ALTER TABLE public.garage_print_settings DROP CONSTRAINT garage_print_settings_document_type_check;

ALTER TABLE public.garage_print_settings
    ADD CONSTRAINT garage_print_settings_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['quote'::character varying, 'repair_order'::character varying, 'settlement'::character varying, 'payment_voucher'::character varying, 'goods_receipt'::character varying, 'stock_issue'::character varying])::text[])));

DELETE FROM public.print_log WHERE document_type IN ('payment_out', 'purchase_order');

ALTER TABLE public.print_log DROP CONSTRAINT print_log_document_type_check;

ALTER TABLE public.print_log
    ADD CONSTRAINT print_log_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['quote'::character varying, 'repair_order'::character varying, 'settlement'::character varying, 'payment_voucher'::character varying, 'goods_receipt'::character varying, 'stock_issue'::character varying])::text[])));
