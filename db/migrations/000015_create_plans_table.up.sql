-- plans: price in VND (no minor unit). Non-public plans (the trial) cannot
-- be chosen by owners; inactive plans are retired.

CREATE TYPE public.billing_cycle AS ENUM (
    'monthly',
    'yearly'
);

CREATE TABLE public.plans (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    code character varying(50) NOT NULL,
    name character varying(100) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    price bigint NOT NULL,
    currency character varying(3) DEFAULT 'VND'::character varying NOT NULL,
    billing_cycle public.billing_cycle DEFAULT 'monthly'::public.billing_cycle NOT NULL,
    max_garages integer DEFAULT 1 NOT NULL,
    max_staff integer NOT NULL,
    max_transactions_per_day integer NOT NULL,
    features public.plan_feature[] DEFAULT '{}'::public.plan_feature[] NOT NULL,
    is_public boolean DEFAULT true NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid,
    CONSTRAINT plans_max_garages_check CHECK ((max_garages > 0)),
    CONSTRAINT plans_max_staff_check CHECK ((max_staff >= 0)),
    CONSTRAINT plans_max_transactions_per_day_check CHECK ((max_transactions_per_day > 0)),
    CONSTRAINT plans_price_check CHECK ((price >= 0))
);

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_code_key UNIQUE (code);

CREATE TRIGGER trigger_plans_row_audit BEFORE INSERT OR UPDATE ON public.plans FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

INSERT INTO plans (id, code, name, description, price, billing_cycle, max_garages, max_staff, max_transactions_per_day, features, is_public, sort_order) VALUES
('00000000-0000-4000-8000-000000000001', 'trial', 'Free trial', '14-day trial with every feature.', 0, 'monthly', 1, 5, 50,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', false, 0),
('00000000-0000-4000-8000-000000000002', 'starter', 'Starter', 'For small garages: customers, quotes, repair orders and billing.', 299000, 'monthly', 1, 5, 50,
 '{billing,customers,documents,quotes,repair_orders}', true, 10),
('00000000-0000-4000-8000-000000000003', 'professional', 'Pro', 'Up to 2 garages, 20 staff and 100 transactions a day.', 599000, 'monthly', 2, 20, 100,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', true, 20),
('00000000-0000-4000-8000-000000000004', 'chain', 'Garage chain', 'For garage chains: up to 10 garages, 100 staff and 1,000 transactions a day.', 1490000, 'monthly', 10, 100, 1000,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', true, 30);
