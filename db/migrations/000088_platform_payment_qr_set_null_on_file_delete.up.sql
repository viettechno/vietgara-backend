-- The payment QR is a file of the platform admin who uploaded it. Deleting that user deletes the
-- file (files.user_id cascades), and the single settings row, which no cascade removes, must not
-- block it: the row keeps its bank details and loses the QR.

ALTER TABLE public.platform_payment_settings
    DROP CONSTRAINT platform_payment_settings_qr_file_id_fkey;

ALTER TABLE public.platform_payment_settings
    ADD CONSTRAINT platform_payment_settings_qr_file_id_fkey FOREIGN KEY (qr_file_id) REFERENCES public.files(id) ON DELETE SET NULL;
