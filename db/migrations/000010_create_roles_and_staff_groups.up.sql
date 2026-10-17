-- Role-based access control (FR-IAM-03, Security Architecture 3.1): an
-- owner defines roles (sets of permissions) shared by all their garages; a
-- staff group assigns one role to members of one garage. Default roles and
-- groups are created by the application when the owner verifies their
-- e-mail and when a garage is created.

-- Permissions are "<module>.<action>", action read, write or (customer
-- only) correct; a write or correct permission includes its module's read
-- permission (enforced by the application when a role is saved or loaded).
CREATE TYPE permission AS ENUM (
    'garage.write',
    'staff.read', 'staff.write',
    'customer.read', 'customer.write', 'customer.correct',
    'supplier.read', 'supplier.write',
    'subscription.read',
    'quote.read', 'quote.write',
    'repair_order.read', 'repair_order.write',
    'inventory.read', 'inventory.write', 'inventory.cost.read',
    'catalogue.read', 'catalogue.write',
    'billing.read', 'billing.write',
    'report.read'
);

CREATE TYPE default_role AS ENUM ('manager', 'accountant', 'service_advisor', 'technician', 'inventory_staff');

-- roles: defined by an owner, shared by all their garages. default_key
-- marks the roles the platform generated, which keeps that step idempotent.
CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    default_key default_role,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_roles_owner_name_unique ON roles (owner_user_id, lower(name));
CREATE UNIQUE INDEX idx_roles_owner_default_unique ON roles (owner_user_id, default_key) WHERE default_key IS NOT NULL;
CREATE TABLE role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission permission NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    PRIMARY KEY (role_id, permission)
);

-- staff_groups: members of one garage sharing a role.
CREATE TABLE staff_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    role_id UUID NOT NULL REFERENCES roles(id),
    default_key default_role,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_staff_groups_garage_name_unique ON staff_groups (garage_id, lower(name));
CREATE UNIQUE INDEX idx_staff_groups_garage_default_unique ON staff_groups (garage_id, default_key) WHERE default_key IS NOT NULL;
CREATE INDEX idx_staff_groups_role_id ON staff_groups (role_id);
-- garage_id is repeated from the group so RLS can filter without a join.
CREATE TABLE staff_group_members (
    group_id UUID NOT NULL REFERENCES staff_groups(id) ON DELETE CASCADE,
    staff_id UUID NOT NULL REFERENCES garage_staff(id) ON DELETE CASCADE,
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    PRIMARY KEY (group_id, staff_id)
);
CREATE INDEX idx_staff_group_members_staff_id ON staff_group_members (staff_id);

-- Row-Level Security; the policies are created in 000013.
ALTER TABLE roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE roles FORCE ROW LEVEL SECURITY;
ALTER TABLE role_permissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE role_permissions FORCE ROW LEVEL SECURITY;
ALTER TABLE staff_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE staff_groups FORCE ROW LEVEL SECURITY;
ALTER TABLE staff_group_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE staff_group_members FORCE ROW LEVEL SECURITY;
