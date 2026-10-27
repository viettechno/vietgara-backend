-- processed_events: Kafka delivers at least once; a consumer records each
-- handled event in the handler's transaction, so a redelivery is skipped
-- (Event Design 6).

CREATE TABLE public.processed_events (
    consumer character varying(100) NOT NULL,
    event_id uuid NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.processed_events
    ADD CONSTRAINT processed_events_pkey PRIMARY KEY (consumer, event_id);
