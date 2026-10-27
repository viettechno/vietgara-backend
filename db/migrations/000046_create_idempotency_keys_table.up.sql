CREATE TABLE public.idempotency_keys (
    garage_id uuid NOT NULL,
    user_id uuid NOT NULL,
    idempotency_key character varying(255) NOT NULL,
    method text NOT NULL,
    request_hash bytea NOT NULL,
    response_type text,
    response bytea,
    http_status integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.idempotency_keys FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (garage_id, user_id, idempotency_key);

CREATE INDEX idx_idempotency_keys_created_at ON public.idempotency_keys USING btree (created_at);

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE public.idempotency_keys ENABLE ROW LEVEL SECURITY;

CREATE POLICY idempotency_keys_rls_policy ON public.idempotency_keys USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY idempotency_keys_rls_policy ON public.idempotency_keys IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
