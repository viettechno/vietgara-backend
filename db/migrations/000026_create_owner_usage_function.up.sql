CREATE FUNCTION public.owner_usage(owner uuid) RETURNS TABLE(garages integer, staff integer)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public'
    AS $$
    SELECT
        (SELECT count(*) FROM garages g WHERE g.user_id = owner AND g.status = 'active')::integer,
        (SELECT count(DISTINCT m.user_id) FROM (
            SELECT s.user_id FROM garage_staff s JOIN garages g ON g.id = s.garage_id
            WHERE g.user_id = owner AND s.user_id <> owner AND s.status = 'active'
            UNION
            SELECT i.user_id FROM staff_invitations i JOIN garages g ON g.id = i.garage_id
            WHERE g.user_id = owner AND i.status = 'pending' AND i.expires_at > now()
        ) m)::integer
$$;

GRANT ALL ON FUNCTION public.owner_usage(owner uuid) TO vietgara_app;
