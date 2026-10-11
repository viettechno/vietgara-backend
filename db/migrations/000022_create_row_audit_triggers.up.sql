-- Row audit columns (NFR-MT-07): every business table carries created_at /
-- created_by, and the tables whose rows can change also updated_at /
-- updated_by. The columns are filled by triggers from the session user
-- (app.current_user_id, set per transaction by the application), so no
-- code path can forget them:
--
--   * set_row_audit() on tables whose rows change: created_by and
--     updated_by come from the session user; created_at and created_by
--     never change; updated_at is the time of the change.
--   * set_created_audit() on append-only tables: created_by comes from the
--     session user.
--
-- A transaction with no session user (a system job: the expiry job, the
-- outbox relay, sign-up) writes NULL. Only when the session has no user,
-- a value the code supplied itself is kept.
--
-- db/audit_columns_test.go fails when a new table has neither the columns
-- and trigger nor an entry in its allowlist.

CREATE OR REPLACE FUNCTION set_row_audit() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.created_by := COALESCE(app_current_user_id(), NEW.created_by);
        NEW.updated_by := COALESCE(app_current_user_id(), NEW.updated_by, NEW.created_by);
    ELSE
        NEW.created_at := OLD.created_at;
        NEW.created_by := OLD.created_by;
        NEW.updated_at := NOW();
        NEW.updated_by := COALESCE(
            app_current_user_id(),
            CASE WHEN NEW.updated_by IS DISTINCT FROM OLD.updated_by THEN NEW.updated_by END);
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION set_created_audit() RETURNS TRIGGER AS $$
BEGIN
    NEW.created_by := COALESCE(app_current_user_id(), NEW.created_by);

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Tables whose rows change.
CREATE TRIGGER trigger_users_row_audit BEFORE INSERT OR UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_plans_row_audit BEFORE INSERT OR UPDATE ON plans FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_subscriptions_row_audit BEFORE INSERT OR UPDATE ON subscriptions FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_entitlements_row_audit BEFORE INSERT OR UPDATE ON entitlements FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_garages_row_audit BEFORE INSERT OR UPDATE ON garages FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_garage_staff_row_audit BEFORE INSERT OR UPDATE ON garage_staff FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_customers_row_audit BEFORE INSERT OR UPDATE ON customers FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_vehicles_row_audit BEFORE INSERT OR UPDATE ON vehicles FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_suppliers_row_audit BEFORE INSERT OR UPDATE ON suppliers FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_roles_row_audit BEFORE INSERT OR UPDATE ON roles FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_staff_groups_row_audit BEFORE INSERT OR UPDATE ON staff_groups FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_staff_invitations_row_audit BEFORE INSERT OR UPDATE ON staff_invitations FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_quotes_row_audit BEFORE INSERT OR UPDATE ON quotes FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_quote_lines_row_audit BEFORE INSERT OR UPDATE ON quote_lines FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_repair_orders_row_audit BEFORE INSERT OR UPDATE ON repair_orders FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_repair_order_lines_row_audit BEFORE INSERT OR UPDATE ON repair_order_lines FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_items_row_audit BEFORE INSERT OR UPDATE ON items FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_stock_balance_row_audit BEFORE INSERT OR UPDATE ON stock_balance FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_settlements_row_audit BEFORE INSERT OR UPDATE ON settlements FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_settlement_payments_row_audit BEFORE INSERT OR UPDATE ON settlement_payments FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_document_sequences_row_audit BEFORE INSERT OR UPDATE ON document_sequences FOR EACH ROW EXECUTE FUNCTION set_row_audit();

-- Append-only tables (and link tables whose rows are only added or removed).
CREATE TRIGGER trigger_platform_admins_created_audit BEFORE INSERT ON platform_admins FOR EACH ROW EXECUTE FUNCTION set_created_audit();
CREATE TRIGGER trigger_role_permissions_created_audit BEFORE INSERT ON role_permissions FOR EACH ROW EXECUTE FUNCTION set_created_audit();
CREATE TRIGGER trigger_staff_group_members_created_audit BEFORE INSERT ON staff_group_members FOR EACH ROW EXECUTE FUNCTION set_created_audit();
CREATE TRIGGER trigger_outbox_events_created_audit BEFORE INSERT ON outbox_events FOR EACH ROW EXECUTE FUNCTION set_created_audit();
CREATE TRIGGER trigger_audit_log_created_audit BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION set_created_audit();
CREATE TRIGGER trigger_stock_transactions_created_audit BEFORE INSERT ON stock_transactions FOR EACH ROW EXECUTE FUNCTION set_created_audit();
CREATE TRIGGER trigger_notification_log_created_audit BEFORE INSERT ON notification_log FOR EACH ROW EXECUTE FUNCTION set_created_audit();
