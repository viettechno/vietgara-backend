ALTER TABLE public.platform_payment_settings
    DROP CONSTRAINT platform_payment_settings_qr_file_id_fkey;

ALTER TABLE public.platform_payment_settings
    ADD CONSTRAINT platform_payment_settings_qr_file_id_fkey FOREIGN KEY (qr_file_id) REFERENCES public.files(id);
