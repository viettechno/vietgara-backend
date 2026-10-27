CREATE TYPE public.outbox_status AS ENUM (
    'pending',
    'published',
    'failed'
);

CREATE TABLE public.outbox_events (
    id uuid NOT NULL,
    event_type character varying(255) NOT NULL,
    garage_id uuid,
    user_id uuid,
    payload jsonb NOT NULL,
    status public.outbox_status DEFAULT 'pending'::public.outbox_status NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    published_at timestamp with time zone,
    created_by uuid
);

ALTER TABLE ONLY public.outbox_events
    ADD CONSTRAINT outbox_events_pkey PRIMARY KEY (id);

CREATE INDEX idx_outbox_events_failed ON public.outbox_events USING btree (created_at) WHERE (status = 'failed'::public.outbox_status);

CREATE INDEX idx_outbox_events_pending ON public.outbox_events USING btree (created_at) WHERE (status = 'pending'::public.outbox_status);

CREATE TRIGGER trigger_outbox_events_created_audit BEFORE INSERT ON public.outbox_events FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();
