-- plans: price in VND (no minor unit). Non-public plans (the trial) cannot
-- be chosen by owners; inactive plans are retired.

CREATE TYPE public.billing_cycle AS ENUM (
    'monthly',
    'yearly'
);

-- The kinds of document a plan may count against max_transactions_per_day.
CREATE TYPE public.transaction_kind AS ENUM (
    'repair_order',
    'quote',
    'settlement'
);

CREATE TABLE public.plans (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    code character varying(50) NOT NULL,
    name character varying(100) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    price bigint NOT NULL,
    currency character varying(3) DEFAULT 'VND'::character varying NOT NULL,
    billing_cycle public.billing_cycle DEFAULT 'yearly'::public.billing_cycle NOT NULL,
    max_garages integer DEFAULT 1 NOT NULL,
    max_staff integer NOT NULL,
    max_transactions_per_day integer NOT NULL,
    counted_transaction_kinds public.transaction_kind[] DEFAULT '{repair_order}'::public.transaction_kind[] NOT NULL,
    trial_days integer,
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
    CONSTRAINT plans_price_check CHECK ((price >= 0)),
    CONSTRAINT plans_trial_days_check CHECK (((trial_days IS NULL) OR (trial_days > 0)))
);

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_code_key UNIQUE (code);

CREATE TRIGGER trigger_plans_row_audit BEFORE INSERT OR UPDATE ON public.plans FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

-- Every plan has every feature; the plans differ in limits. Prices are per
-- year in VND. Only the trial plan has trial_days.
INSERT INTO plans (id, code, name, description, price, billing_cycle, max_garages, max_staff, max_transactions_per_day, trial_days, features, is_public, sort_order) VALUES
('00000000-0000-4000-8000-000000000001', 'trial', 'Free trial', '2-month trial with every feature.', 0, 'yearly', 1, 5, 50, 60,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', false, 0),
('00000000-0000-4000-8000-000000000002', 'starter', 'Starter', 'For a single garage: 1 garage, 5 staff and 50 repair orders a day, with every feature.', 2990000, 'yearly', 1, 5, 50, NULL,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', true, 10),
('00000000-0000-4000-8000-000000000003', 'professional', 'Pro', 'Up to 3 garages, 20 staff and 150 repair orders a day, with every feature.', 5990000, 'yearly', 3, 20, 150, NULL,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', true, 20),
('00000000-0000-4000-8000-000000000004', 'chain', 'Garage chain', 'For garage chains: up to 10 garages, 100 staff and 1,000 repair orders a day, with every feature.', 14900000, 'yearly', 10, 100, 1000, NULL,
 '{billing,customers,documents,inventory,quotes,repair_orders,reports}', true, 30);
