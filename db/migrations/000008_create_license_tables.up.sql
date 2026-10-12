-- License Management (HLD module #3, Database Design 6.3): one subscription
-- per owner user covering all their garages; plans limit garages, staff
-- and business transactions per day (FR-LIC-01/02/05). These tables are
-- platform-level data without Row-Level Security; the License module
-- authorizes access.

CREATE TYPE billing_cycle AS ENUM ('monthly', 'yearly');
CREATE TYPE plan_feature AS ENUM ('customers', 'quotes', 'repair_orders', 'inventory', 'billing', 'documents', 'reports');
CREATE TYPE subscription_status AS ENUM ('trial', 'active', 'past_due', 'suspended', 'cancelled');

-- ---------------------------------------------------------------------------
-- plans: price in VND (no minor unit). Non-public plans (the trial) cannot
-- be chosen by owners; inactive plans are retired.
-- ---------------------------------------------------------------------------
CREATE TABLE plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(50) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price BIGINT NOT NULL CHECK (price >= 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'VND',
    billing_cycle billing_cycle NOT NULL DEFAULT 'monthly',
    max_garages INTEGER NOT NULL DEFAULT 1 CHECK (max_garages > 0),
    max_staff INTEGER NOT NULL CHECK (max_staff >= 0),
    max_transactions_per_day INTEGER NOT NULL CHECK (max_transactions_per_day > 0),
    features plan_feature[] NOT NULL DEFAULT '{}',
    is_public BOOLEAN NOT NULL DEFAULT true,
    is_active BOOLEAN NOT NULL DEFAULT true,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID
);

-- Initial catalogue (garages / staff / transactions per day). Prices are
-- placeholders until Product confirms them; platform admins edit plans
-- through the API (FR-LIC-05).
INSERT INTO plans (id, code, name, description, price, billing_cycle, max_garages, max_staff, max_transactions_per_day, features, is_public, sort_order) VALUES
('00000000-0000-4000-8000-000000000001', 'trial', 'Free trial', '14-day trial with every feature.', 0, 'monthly', 1, 5, 50,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', false, 0),
('00000000-0000-4000-8000-000000000002', 'starter', 'Starter', 'For small garages: customers, quotes, repair orders and billing.', 299000, 'monthly', 1, 5, 50,
 '{billing,customers,documents,quotes,repair_orders}', true, 10),
('00000000-0000-4000-8000-000000000003', 'professional', 'Pro', 'Up to 2 garages, 20 staff and 100 transactions a day.', 599000, 'monthly', 2, 20, 100,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', true, 20),
('00000000-0000-4000-8000-000000000004', 'chain', 'Garage chain', 'For garage chains: up to 10 garages, 100 staff and 1,000 transactions a day.', 1490000, 'monthly', 10, 100, 1000,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', true, 30);

-- ---------------------------------------------------------------------------
-- subscriptions: one per owner user (LLD Section 3.1).
-- reminder_period_end remembers which period end the expiring-soon reminder
-- was already sent for, so the expiry job sends it once.
-- ---------------------------------------------------------------------------
CREATE TABLE subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    plan_id UUID NOT NULL REFERENCES plans(id),
    status subscription_status NOT NULL,
    trial_end_at TIMESTAMP WITH TIME ZONE,
    current_period_start TIMESTAMP WITH TIME ZONE NOT NULL,
    current_period_end TIMESTAMP WITH TIME ZONE NOT NULL,
    grace_period_end TIMESTAMP WITH TIME ZONE,
    auto_renew BOOLEAN NOT NULL DEFAULT true,
    reminder_period_end TIMESTAMP WITH TIME ZONE,
    -- What the owner paid for the current period (VND; 0 for a trial): the
    -- basis of the credit when the plan changes (FR-LIC-09).
    period_paid_amount BIGINT NOT NULL DEFAULT 0 CHECK (period_paid_amount >= 0),
    -- Account credit, applied automatically to the next payment.
    credit_balance BIGINT NOT NULL DEFAULT 0 CHECK (credit_balance >= 0),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID
);

-- The expiry job's scan: live subscriptions by period end
CREATE INDEX idx_subscriptions_period_end ON subscriptions (current_period_end) WHERE status IN ('trial', 'active', 'past_due');

-- ---------------------------------------------------------------------------
-- entitlements: per-user feature switches derived from the
-- subscription's plan and status (source of the entitlement cache).
-- ---------------------------------------------------------------------------
CREATE TABLE entitlements (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feature plan_feature NOT NULL,
    enabled BOOLEAN NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID,
    PRIMARY KEY (user_id, feature)
);
