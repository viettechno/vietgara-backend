ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM vietgara_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE USAGE, SELECT ON SEQUENCES FROM vietgara_app;
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM vietgara_app;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM vietgara_app;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM vietgara_app;
REVOKE USAGE ON SCHEMA public FROM vietgara_app;
-- The role itself is kept: login users may still be members of it.

DROP FUNCTION IF EXISTS owner_usage(UUID);

DROP POLICY IF EXISTS staff_invitations_delete ON staff_invitations;
DROP POLICY IF EXISTS staff_invitations_update ON staff_invitations;
DROP POLICY IF EXISTS staff_invitations_insert ON staff_invitations;
DROP POLICY IF EXISTS staff_invitations_select ON staff_invitations;

DROP POLICY IF EXISTS role_permissions_write ON role_permissions;
DROP POLICY IF EXISTS role_permissions_select ON role_permissions;
DROP POLICY IF EXISTS roles_write ON roles;
DROP POLICY IF EXISTS roles_select ON roles;

DROP POLICY IF EXISTS staff_groups_write ON staff_groups;
DROP POLICY IF EXISTS staff_groups_select ON staff_groups;
DROP POLICY IF EXISTS staff_group_members_write ON staff_group_members;
DROP POLICY IF EXISTS staff_group_members_select ON staff_group_members;

DROP POLICY IF EXISTS garage_staff_delete ON garage_staff;
DROP POLICY IF EXISTS garage_staff_update ON garage_staff;
DROP POLICY IF EXISTS garage_staff_insert ON garage_staff;
DROP POLICY IF EXISTS garage_staff_select ON garage_staff;

DROP POLICY IF EXISTS garages_delete ON garages;
DROP POLICY IF EXISTS garages_update ON garages;
DROP POLICY IF EXISTS garages_insert ON garages;
DROP POLICY IF EXISTS garages_select ON garages;
