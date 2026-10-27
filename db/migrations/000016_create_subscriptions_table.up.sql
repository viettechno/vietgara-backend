-- subscriptions: one per owner user (LLD Section 3.1).
-- reminder_period_end remembers which period end the expiring-soon reminder
-- was already sent for, so the expiry job sends it once.

CREATE TYPE public.subscription_status AS ENUM (
    'trial',
    'active',
    'past_due',
    'suspended',
    'cancelled'
);

CREATE TABLE public.subscriptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    plan_id uuid NOT NULL,
    status public.subscription_status NOT NULL,
    trial_end_at timestamp with time zone,
    current_period_start timestamp with time zone NOT NULL,
    current_period_end timestamp with time zone NOT NULL,
    grace_period_end timestamp with time zone,
    auto_renew boolean DEFAULT true NOT NULL,
    reminder_period_end timestamp with time zone,
    period_paid_amount bigint DEFAULT 0 NOT NULL,
    credit_balance bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid,
    CONSTRAINT subscriptions_credit_balance_check CHECK ((credit_balance >= 0)),
    CONSTRAINT subscriptions_period_paid_amount_check CHECK ((period_paid_amount >= 0))
);

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_user_id_key UNIQUE (user_id);

CREATE INDEX idx_subscriptions_period_end ON public.subscriptions USING btree (current_period_end) WHERE (status = ANY (ARRAY['trial'::public.subscription_status, 'active'::public.subscription_status, 'past_due'::public.subscription_status]));

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id);

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_subscriptions_row_audit BEFORE INSERT OR UPDATE ON public.subscriptions FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();
