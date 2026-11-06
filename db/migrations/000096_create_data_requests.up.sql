-- Personal-data requests (Release 13.1, FR-CUS-05, PD-06): the garage's record
-- of a request to view, correct or delete a customer's data and of its answer,
-- the requests of people without the app that wait for their e-mail link, and
-- the mark of an anonymized customer.

ALTER TABLE public.customers ADD COLUMN anonymized_at timestamp with time zone;

-- The notification log is append-only; anonymizing a customer is the one thing
-- that changes it: the recipient address of the customer's rows is cleared.
GRANT UPDATE (recipient) ON public.notification_log TO vietgara_app;

CREATE TYPE public.data_request_type AS ENUM ('view', 'correct', 'delete');

CREATE TYPE public.data_request_status AS ENUM ('pending', 'completed', 'rejected');

CREATE TYPE public.data_request_source AS ENUM ('staff', 'public');

CREATE TABLE public.customer_data_requests (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    type public.data_request_type NOT NULL,
    status public.data_request_status DEFAULT 'pending'::public.data_request_status NOT NULL,
    source public.data_request_source NOT NULL,
    details text DEFAULT ''::text NOT NULL,
    outcome text DEFAULT ''::text NOT NULL,
    requested_by uuid,
    requested_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    completed_by uuid,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT customer_data_requests_details_check CHECK ((char_length(details) <= 2000)),
    CONSTRAINT customer_data_requests_outcome_check CHECK ((char_length(outcome) <= 2000))
);

ALTER TABLE ONLY public.customer_data_requests FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.customer_data_requests
    ADD CONSTRAINT customer_data_requests_pkey PRIMARY KEY (id);

CREATE INDEX idx_customer_data_requests_garage ON public.customer_data_requests USING btree (garage_id, requested_at DESC);

CREATE INDEX idx_customer_data_requests_customer ON public.customer_data_requests USING btree (garage_id, customer_id);

ALTER TABLE ONLY public.customer_data_requests
    ADD CONSTRAINT customer_data_requests_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_customer_data_requests_row_audit BEFORE INSERT OR UPDATE ON public.customer_data_requests FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.customer_data_requests ENABLE ROW LEVEL SECURITY;

CREATE POLICY customer_data_requests_rls_policy ON public.customer_data_requests USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY customer_data_requests_rls_policy ON public.customer_data_requests IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- A public request waits here for its e-mail link. It belongs to no garage: the
-- person is known only by the address. The token is stored as a hash, the row
-- is deleted after 7 days with its details, and nothing is stored once the
-- request has acted.
CREATE TABLE public.data_request_confirmations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email character varying(255) NOT NULL,
    type public.data_request_type NOT NULL,
    details text DEFAULT ''::text NOT NULL,
    locale character varying(2) DEFAULT 'vi'::character varying NOT NULL,
    token_hash character varying(64) NOT NULL,
    source_hash character varying(64) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT data_request_confirmations_details_check CHECK ((char_length(details) <= 2000))
);

ALTER TABLE ONLY public.data_request_confirmations
    ADD CONSTRAINT data_request_confirmations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.data_request_confirmations
    ADD CONSTRAINT data_request_confirmations_token_hash_key UNIQUE (token_hash);

CREATE INDEX idx_data_request_confirmations_email ON public.data_request_confirmations USING btree (lower((email)::text), created_at);

CREATE INDEX idx_data_request_confirmations_source ON public.data_request_confirmations USING btree (source_hash, created_at);

CREATE INDEX idx_data_request_confirmations_created ON public.data_request_confirmations USING btree (created_at);
