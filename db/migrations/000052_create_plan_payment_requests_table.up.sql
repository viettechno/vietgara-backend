CREATE TYPE public.plan_payment_kind AS ENUM (
    'change',
    'renew'
);

CREATE TYPE public.plan_payment_status AS ENUM (
    'awaiting_payment',
    'submitted',
    'approved',
    'rejected',
    'cancelled'
);

CREATE TABLE public.plan_payment_requests (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    subscription_id uuid NOT NULL,
    kind public.plan_payment_kind NOT NULL,
    plan_id uuid NOT NULL,
    status public.plan_payment_status DEFAULT 'awaiting_payment'::public.plan_payment_status NOT NULL,
    reference character varying(20) NOT NULL,
    quoted_amount bigint NOT NULL,
    credit_applied bigint NOT NULL,
    amount_due bigint NOT NULL,
    credit_surplus bigint NOT NULL,
    unused_credit bigint DEFAULT 0 NOT NULL,
    period_days integer NOT NULL,
    unused_days integer NOT NULL,
    instructions jsonb DEFAULT '{}'::jsonb NOT NULL,
    payer_name character varying(255),
    payer_bank character varying(255),
    payer_account character varying(100),
    transferred_amount bigint,
    bank_transaction_ref character varying(100),
    paid_at timestamp with time zone,
    evidence_file_id uuid,
    amount_mismatch boolean DEFAULT false NOT NULL,
    review_note text,
    rejection_reason text,
    reviewed_by uuid,
    reviewed_at timestamp with time zone,
    submitted_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT plan_payment_requests_amount_due_check CHECK ((amount_due >= 0)),
    CONSTRAINT plan_payment_requests_credit_applied_check CHECK ((credit_applied >= 0)),
    CONSTRAINT plan_payment_requests_credit_surplus_check CHECK ((credit_surplus >= 0)),
    CONSTRAINT plan_payment_requests_quoted_amount_check CHECK ((quoted_amount >= 0)),
    CONSTRAINT plan_payment_requests_transferred_amount_check CHECK (((transferred_amount IS NULL) OR (transferred_amount >= 0))),
    CONSTRAINT plan_payment_requests_unused_credit_check CHECK ((unused_credit >= 0))
);

ALTER TABLE ONLY public.plan_payment_requests
    ADD CONSTRAINT plan_payment_requests_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.plan_payment_requests
    ADD CONSTRAINT plan_payment_requests_reference_key UNIQUE (reference);

CREATE UNIQUE INDEX idx_plan_payment_requests_open ON public.plan_payment_requests USING btree (user_id) WHERE (status = ANY (ARRAY['awaiting_payment'::public.plan_payment_status, 'submitted'::public.plan_payment_status]));

CREATE INDEX idx_plan_payment_requests_review ON public.plan_payment_requests USING btree (submitted_at) WHERE (status = 'submitted'::public.plan_payment_status);

CREATE INDEX idx_plan_payment_requests_user ON public.plan_payment_requests USING btree (user_id, created_at DESC);

ALTER TABLE ONLY public.plan_payment_requests
    ADD CONSTRAINT plan_payment_requests_evidence_file_id_fkey FOREIGN KEY (evidence_file_id) REFERENCES public.files(id);

ALTER TABLE ONLY public.plan_payment_requests
    ADD CONSTRAINT plan_payment_requests_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id);

ALTER TABLE ONLY public.plan_payment_requests
    ADD CONSTRAINT plan_payment_requests_subscription_id_fkey FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.plan_payment_requests
    ADD CONSTRAINT plan_payment_requests_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_plan_payment_requests_row_audit BEFORE INSERT OR UPDATE ON public.plan_payment_requests FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();
