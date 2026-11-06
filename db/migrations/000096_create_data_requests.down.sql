DROP TABLE IF EXISTS public.data_request_confirmations;
DROP TABLE IF EXISTS public.customer_data_requests;
DROP TYPE IF EXISTS public.data_request_source;
DROP TYPE IF EXISTS public.data_request_status;
DROP TYPE IF EXISTS public.data_request_type;

REVOKE UPDATE (recipient) ON public.notification_log FROM vietgara_app;

ALTER TABLE public.customers DROP COLUMN IF EXISTS anonymized_at;
