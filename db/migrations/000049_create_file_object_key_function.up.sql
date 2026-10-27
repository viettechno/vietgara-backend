CREATE FUNCTION public.file_object_key(file_id uuid) RETURNS TABLE(key character varying, content_type character varying)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public'
    AS $$
    SELECT f.key, f.content_type FROM files f WHERE f.id = file_id AND f.deleted_at IS NULL
$$;

GRANT ALL ON FUNCTION public.file_object_key(file_id uuid) TO vietgara_app;
