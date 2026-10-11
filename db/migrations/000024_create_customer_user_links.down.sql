DROP POLICY IF EXISTS customer_user_links_rls_policy ON customer_user_links;
DROP TRIGGER IF EXISTS trigger_customer_user_links_row_audit ON customer_user_links;
DROP TABLE IF EXISTS customer_user_links;

DROP TYPE IF EXISTS customer_user_link_channel;
DROP TYPE IF EXISTS customer_user_link_status;
