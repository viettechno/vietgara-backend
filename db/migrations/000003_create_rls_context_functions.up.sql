-- The transaction's garage and user, set by the backend (app.current_garage_id,
-- app.current_user_id); every row-level security policy reads them.
CREATE FUNCTION public.app_current_garage_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT NULLIF(current_setting('app.current_garage_id', true), '')::uuid $$;

GRANT ALL ON FUNCTION public.app_current_garage_id() TO vietgara_app;

CREATE FUNCTION public.app_current_user_id() RETURNS uuid
    LANGUAGE sql STABLE
    AS $$ SELECT NULLIF(current_setting('app.current_user_id', true), '')::uuid $$;

GRANT ALL ON FUNCTION public.app_current_user_id() TO vietgara_app;
