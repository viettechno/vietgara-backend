-- The payment-out voucher and the purchase order are printable documents
-- (Release 13.0): the print settings and the print log accept them.

ALTER TABLE public.garage_print_settings DROP CONSTRAINT garage_print_settings_document_type_check;

ALTER TABLE public.garage_print_settings
    ADD CONSTRAINT garage_print_settings_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['quote'::character varying, 'repair_order'::character varying, 'settlement'::character varying, 'payment_voucher'::character varying, 'goods_receipt'::character varying, 'stock_issue'::character varying, 'payment_out'::character varying, 'purchase_order'::character varying])::text[]))) NOT VALID;

ALTER TABLE public.garage_print_settings VALIDATE CONSTRAINT garage_print_settings_document_type_check;

ALTER TABLE public.print_log DROP CONSTRAINT print_log_document_type_check;

ALTER TABLE public.print_log
    ADD CONSTRAINT print_log_document_type_check CHECK (((document_type)::text = ANY ((ARRAY['quote'::character varying, 'repair_order'::character varying, 'settlement'::character varying, 'payment_voucher'::character varying, 'goods_receipt'::character varying, 'stock_issue'::character varying, 'payment_out'::character varying, 'purchase_order'::character varying])::text[]))) NOT VALID;

ALTER TABLE public.print_log VALIDATE CONSTRAINT print_log_document_type_check;
