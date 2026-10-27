CREATE FUNCTION public.find_quote_by_public_token(token_hash text) RETURNS TABLE(id uuid, garage_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public'
    AS $$
    SELECT q.id, q.garage_id FROM quotes q
    WHERE q.public_token_hash = token_hash
      AND q.public_token_expires_at > now()
      AND q.deleted_at IS NULL
$$;

GRANT ALL ON FUNCTION public.find_quote_by_public_token(token_hash text) TO vietgara_app;
