-- Platform roles, staff groups and invitations (Release 8.1, FR-IAM-10).
--
-- The platform scope mirrors the garage scope (roles, staff groups, group
-- members, invitations) but belongs to no garage: there is no garage_id and
-- no row-level security. The platform permission catalogue is fixed. A user's
-- platform permissions are the union of the roles of the groups they belong
-- to, read on every request (Security Architecture 3.4); platform_admins keeps
-- the Platform Admins configured in the deployment, who hold every permission.

CREATE TYPE platform_permission AS ENUM (
    'plan.read', 'plan.write',
    'subscription.read', 'subscription.write',
    'stats.read',
    'garage_directory.read',
    'user.read',
    'platform_staff.read', 'platform_staff.write',
    'support.read', 'support.write',
    'audit.read'
);

CREATE TABLE platform_roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID
);
CREATE UNIQUE INDEX idx_platform_roles_name ON platform_roles (lower(name));

CREATE TABLE platform_role_permissions (
    role_id UUID NOT NULL REFERENCES platform_roles(id) ON DELETE CASCADE,
    permission platform_permission NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    PRIMARY KEY (role_id, permission)
);

CREATE TABLE platform_staff_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- A group has one role, like a garage staff group. The role cannot be
    -- deleted while a group uses it.
    role_id UUID NOT NULL REFERENCES platform_roles(id) ON DELETE RESTRICT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID
);
CREATE UNIQUE INDEX idx_platform_staff_groups_name ON platform_staff_groups (lower(name));
CREATE INDEX idx_platform_staff_groups_role_id ON platform_staff_groups (role_id);

CREATE TABLE platform_staff_group_members (
    group_id UUID NOT NULL REFERENCES platform_staff_groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX idx_platform_staff_group_members_user_id ON platform_staff_group_members (user_id);

-- The same shape as staff_invitations without a garage: only existing users
-- can be invited, the invitee is e-mailed and joins the group on acceptance.
-- One pending invitation per group and user; they expire after 7 days.
CREATE TABLE platform_staff_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES platform_staff_groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status invitation_status NOT NULL DEFAULT 'pending',
    invited_by UUID NOT NULL REFERENCES users(id),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    responded_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID
);
CREATE UNIQUE INDEX idx_platform_staff_invitations_pending_unique ON platform_staff_invitations (group_id, user_id) WHERE status = 'pending';
CREATE INDEX idx_platform_staff_invitations_user_id ON platform_staff_invitations (user_id, created_at DESC);
CREATE INDEX idx_platform_staff_invitations_group_id ON platform_staff_invitations (group_id, created_at DESC);

CREATE TRIGGER trigger_platform_roles_row_audit BEFORE INSERT OR UPDATE ON platform_roles FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_platform_staff_groups_row_audit BEFORE INSERT OR UPDATE ON platform_staff_groups FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_platform_staff_invitations_row_audit BEFORE INSERT OR UPDATE ON platform_staff_invitations FOR EACH ROW EXECUTE FUNCTION set_row_audit();
CREATE TRIGGER trigger_platform_role_permissions_created_audit BEFORE INSERT ON platform_role_permissions FOR EACH ROW EXECUTE FUNCTION set_created_audit();
CREATE TRIGGER trigger_platform_staff_group_members_created_audit BEFORE INSERT ON platform_staff_group_members FOR EACH ROW EXECUTE FUNCTION set_created_audit();
