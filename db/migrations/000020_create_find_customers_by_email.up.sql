-- Customer Portal (Customer BFF, Release 2.4): the Customer App's
-- signed-in person -- a VietGara user created by Google sign-in
-- (FR-CAPP-01) -- is matched to garages' customer records by the user's
-- verified e-mail. That person is not a member of any of those garages, so
-- the usual garage-scoped RLS cannot find their records yet; the same
-- problem find_quote_by_public_token() (000015) solves for the public
-- approval link. This function runs SECURITY DEFINER and returns only the
-- ids the caller needs to open a normally-scoped transaction per garage;
-- it never returns customer data.

-- Case-insensitive e-mail lookups across garages.
CREATE INDEX idx_customers_email_lower ON customers (lower(email))
    WHERE deleted_at IS NULL AND email IS NOT NULL;

CREATE OR REPLACE FUNCTION find_customers_by_email(customer_email TEXT)
RETURNS TABLE (id UUID, garage_id UUID)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
    SELECT c.id, c.garage_id FROM customers c
    WHERE lower(c.email) = lower(customer_email)
      AND c.deleted_at IS NULL
$$;

GRANT EXECUTE ON FUNCTION find_customers_by_email(TEXT) TO vietgara_app;
