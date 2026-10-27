CREATE TYPE public.customer_user_link_channel AS ENUM (
    'otp',
    'app',
    'auto_email'
);

CREATE TYPE public.customer_user_link_status AS ENUM (
    'requested',
    'linked',
    'declined',
    'unlinked'
);

CREATE TABLE public.customer_user_links (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status public.customer_user_link_status DEFAULT 'requested'::public.customer_user_link_status NOT NULL,
    channel public.customer_user_link_channel,
    requested_at timestamp with time zone DEFAULT now() NOT NULL,
    linked_at timestamp with time zone,
    ended_at timestamp with time zone,
    created_by uuid,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT customer_user_links_linked_check CHECK (((status <> 'linked'::public.customer_user_link_status) OR ((channel IS NOT NULL) AND (linked_at IS NOT NULL))))
);

ALTER TABLE ONLY public.customer_user_links FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.customer_user_links
    ADD CONSTRAINT customer_user_links_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX idx_customer_user_links_current ON public.customer_user_links USING btree (customer_id) WHERE (status = ANY (ARRAY['requested'::public.customer_user_link_status, 'linked'::public.customer_user_link_status]));

CREATE INDEX idx_customer_user_links_customer ON public.customer_user_links USING btree (garage_id, customer_id, created_at DESC);

CREATE INDEX idx_customer_user_links_user ON public.customer_user_links USING btree (user_id, status);

ALTER TABLE ONLY public.customer_user_links
    ADD CONSTRAINT customer_user_links_customer_fkey FOREIGN KEY (garage_id, customer_id) REFERENCES public.customers(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.customer_user_links
    ADD CONSTRAINT customer_user_links_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_customer_user_links_row_audit BEFORE INSERT OR UPDATE ON public.customer_user_links FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.customer_user_links ENABLE ROW LEVEL SECURITY;

CREATE POLICY customer_user_links_rls_policy ON public.customer_user_links USING (((garage_id = public.app_current_garage_id()) OR (user_id = public.app_current_user_id()))) WITH CHECK (((garage_id = public.app_current_garage_id()) OR (user_id = public.app_current_user_id())));

COMMENT ON POLICY customer_user_links_rls_policy ON public.customer_user_links IS 'The transaction''s garage, or the link''s own user (app.current_user_id, see app_current_user_id()).';
