ALTER TABLE public.customers
    DROP CONSTRAINT IF EXISTS customers_locale_check,
    DROP COLUMN IF EXISTS email_opt_out_at,
    DROP COLUMN IF EXISTS locale;
