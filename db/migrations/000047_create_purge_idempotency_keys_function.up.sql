CREATE FUNCTION public.purge_idempotency_keys(older_than timestamp with time zone) RETURNS bigint
    LANGUAGE sql SECURITY DEFINER
    SET search_path TO 'public'
    AS $$
    WITH deleted AS (
        DELETE FROM idempotency_keys WHERE created_at < older_than RETURNING 1
    )
    SELECT count(*) FROM deleted
$$;

GRANT ALL ON FUNCTION public.purge_idempotency_keys(older_than timestamp with time zone) TO vietgara_app;
