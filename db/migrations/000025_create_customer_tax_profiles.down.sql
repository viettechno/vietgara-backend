DROP POLICY IF EXISTS customer_tax_profiles_rls_policy ON customer_tax_profiles;
DROP TRIGGER IF EXISTS trigger_customer_tax_profiles_row_audit ON customer_tax_profiles;
DROP TABLE IF EXISTS customer_tax_profiles;

DROP TYPE IF EXISTS customer_tax_profile_kind;
