-- Row-Level Security policies of the tenant/access tables (Database Design
-- 7, Security Architecture 4). They are created here, after every table
-- they reference exists. Writes are limited to the transaction's garage
-- (app_current_garage_id()) or to the caller's own roles; reads also admit
-- the caller's own memberships and invitations (app_current_account_id()),
-- so an account can list its garages without a tenant context.
-- customers, vehicles and partners carry their policies in 000005-000007.

-- ---------------------------------------------------------------------------
-- garages
-- ---------------------------------------------------------------------------
CREATE POLICY garages_select ON garages FOR SELECT USING (
    id = app_current_garage_id()
    OR account_id = app_current_account_id()
    OR EXISTS (SELECT 1 FROM garage_staff s WHERE s.garage_id = garages.id AND s.account_id = app_current_account_id())
    OR EXISTS (SELECT 1 FROM staff_invitations i WHERE i.garage_id = garages.id AND i.account_id = app_current_account_id())
);
CREATE POLICY garages_insert ON garages FOR INSERT WITH CHECK (id = app_current_garage_id() AND account_id = app_current_account_id());
CREATE POLICY garages_update ON garages FOR UPDATE USING (id = app_current_garage_id()) WITH CHECK (id = app_current_garage_id());
CREATE POLICY garages_delete ON garages FOR DELETE USING (id = app_current_garage_id());

-- ---------------------------------------------------------------------------
-- garage_staff
-- ---------------------------------------------------------------------------
CREATE POLICY garage_staff_select ON garage_staff FOR SELECT USING (
    garage_id = app_current_garage_id() OR account_id = app_current_account_id()
);
CREATE POLICY garage_staff_insert ON garage_staff FOR INSERT WITH CHECK (garage_id = app_current_garage_id());
CREATE POLICY garage_staff_update ON garage_staff FOR UPDATE USING (garage_id = app_current_garage_id()) WITH CHECK (garage_id = app_current_garage_id());
CREATE POLICY garage_staff_delete ON garage_staff FOR DELETE USING (garage_id = app_current_garage_id());

-- ---------------------------------------------------------------------------
-- staff_group_members, staff_groups
-- ---------------------------------------------------------------------------
CREATE POLICY staff_group_members_select ON staff_group_members FOR SELECT USING (
    garage_id = app_current_garage_id()
    OR EXISTS (SELECT 1 FROM garage_staff s WHERE s.id = staff_group_members.staff_id AND s.account_id = app_current_account_id())
);
CREATE POLICY staff_group_members_write ON staff_group_members FOR ALL
    USING (garage_id = app_current_garage_id()) WITH CHECK (garage_id = app_current_garage_id());

CREATE POLICY staff_groups_select ON staff_groups FOR SELECT USING (
    garage_id = app_current_garage_id()
    OR EXISTS (SELECT 1 FROM garages g WHERE g.id = staff_groups.garage_id AND g.account_id = app_current_account_id())
    OR EXISTS (SELECT 1 FROM staff_group_members m WHERE m.group_id = staff_groups.id)
);
CREATE POLICY staff_groups_write ON staff_groups FOR ALL
    USING (garage_id = app_current_garage_id()) WITH CHECK (garage_id = app_current_garage_id());

-- ---------------------------------------------------------------------------
-- roles, role_permissions: owned by an account; visible to the members of
-- the owner's garages through their groups.
-- ---------------------------------------------------------------------------
CREATE POLICY roles_select ON roles FOR SELECT USING (
    owner_account_id = app_current_account_id()
    OR owner_account_id = (SELECT g.account_id FROM garages g WHERE g.id = app_current_garage_id())
    OR EXISTS (SELECT 1 FROM staff_groups sg WHERE sg.role_id = roles.id)
);
CREATE POLICY roles_write ON roles FOR ALL
    USING (owner_account_id = app_current_account_id()) WITH CHECK (owner_account_id = app_current_account_id());

CREATE POLICY role_permissions_select ON role_permissions FOR SELECT USING (
    EXISTS (SELECT 1 FROM roles r WHERE r.id = role_permissions.role_id)
);
CREATE POLICY role_permissions_write ON role_permissions FOR ALL
    USING (EXISTS (SELECT 1 FROM roles r WHERE r.id = role_permissions.role_id AND r.owner_account_id = app_current_account_id()))
    WITH CHECK (EXISTS (SELECT 1 FROM roles r WHERE r.id = role_permissions.role_id AND r.owner_account_id = app_current_account_id()));

-- ---------------------------------------------------------------------------
-- staff_invitations: the garage side creates and revokes; the invitee
-- accepts or declines.
-- ---------------------------------------------------------------------------
CREATE POLICY staff_invitations_select ON staff_invitations FOR SELECT USING (
    garage_id = app_current_garage_id() OR account_id = app_current_account_id()
);
CREATE POLICY staff_invitations_insert ON staff_invitations FOR INSERT WITH CHECK (garage_id = app_current_garage_id());
CREATE POLICY staff_invitations_update ON staff_invitations FOR UPDATE
    USING (garage_id = app_current_garage_id() OR account_id = app_current_account_id())
    WITH CHECK (garage_id = app_current_garage_id() OR account_id = app_current_account_id());
CREATE POLICY staff_invitations_delete ON staff_invitations FOR DELETE USING (garage_id = app_current_garage_id());

-- ---------------------------------------------------------------------------
-- owner_usage: what an owner uses of their plan limits (FR-LIC-02) across
-- all their garages. A manager inviting staff cannot see the owner's other
-- garages through RLS, so this aggregate-only function runs with its
-- owner's rights (SECURITY DEFINER) and returns counts only. Staff are
-- distinct non-owner accounts with an active membership or a live pending
-- invitation; garages count while active.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION owner_usage(owner UUID)
RETURNS TABLE (garages INTEGER, staff INTEGER)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public AS $$
    SELECT
        (SELECT count(*) FROM garages g WHERE g.account_id = owner AND g.status = 'active')::integer,
        (SELECT count(DISTINCT m.account_id) FROM (
            SELECT s.account_id FROM garage_staff s JOIN garages g ON g.id = s.garage_id
            WHERE g.account_id = owner AND s.account_id <> owner AND s.status = 'active'
            UNION
            SELECT i.account_id FROM staff_invitations i JOIN garages g ON g.id = i.garage_id
            WHERE g.account_id = owner AND i.status = 'pending' AND i.expires_at > now()
        ) m)::integer
$$;

-- ---------------------------------------------------------------------------
-- vietgara_app: the role the backend connects as (via a LOGIN user that is
-- a member of it). It is neither superuser nor BYPASSRLS, so every FORCE
-- ROW LEVEL SECURITY policy applies to it (NFR-SE-03). audit_log is
-- append-only for it.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'vietgara_app') THEN
        CREATE ROLE vietgara_app NOLOGIN NOSUPERUSER NOBYPASSRLS;
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO vietgara_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO vietgara_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO vietgara_app;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO vietgara_app;
REVOKE UPDATE, DELETE ON audit_log FROM vietgara_app;
DO $$
BEGIN
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        REVOKE ALL ON schema_migrations FROM vietgara_app;
    END IF;
END
$$;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO vietgara_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO vietgara_app;
