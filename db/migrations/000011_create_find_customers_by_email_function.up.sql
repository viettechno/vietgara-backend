CREATE FUNCTION public.find_customers_by_email(customer_email text) RETURNS TABLE(id uuid, garage_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public'
    AS $$
    SELECT c.id, c.garage_id FROM customers c
    WHERE lower(c.email) = lower(customer_email)
      AND c.deleted_at IS NULL
$$;

GRANT ALL ON FUNCTION public.find_customers_by_email(customer_email text) TO vietgara_app;
