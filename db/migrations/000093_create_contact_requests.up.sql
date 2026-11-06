-- contact_requests: contact and demo requests from the public website
-- (FR-WEB-02, FR-WEB-04). A platform table: no garage or user key, so no row
-- level security (listed in noRLS of the coverage test). source_ip_hash is a
-- keyed hash, never the address.

CREATE TABLE public.contact_requests (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(120) NOT NULL,
    garage_name character varying(160),
    phone character varying(32) NOT NULL,
    email character varying(254),
    message text,
    locale character varying(2) NOT NULL,
    consent_at timestamp with time zone NOT NULL,
    source_ip_hash character varying(64) NOT NULL,
    user_agent character varying(300),
    status character varying(16) DEFAULT 'new'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    CONSTRAINT contact_requests_locale_check CHECK (((locale)::text = ANY ((ARRAY['vi'::character varying, 'en'::character varying])::text[]))),
    CONSTRAINT contact_requests_status_check CHECK (((status)::text = ANY ((ARRAY['new'::character varying, 'handled'::character varying])::text[])))
);

ALTER TABLE ONLY public.contact_requests
    ADD CONSTRAINT contact_requests_pkey PRIMARY KEY (id);

CREATE TRIGGER trigger_contact_requests_created_audit BEFORE INSERT ON public.contact_requests FOR EACH ROW EXECUTE FUNCTION public.set_created_audit();

-- The rate limit counts the requests of one source in the last hour.
CREATE INDEX idx_contact_requests_source ON public.contact_requests USING btree (source_ip_hash, created_at DESC);

-- Insert and read only; support marks a request handled by hand until a
-- screen exists, which is the one column the application role may change.
REVOKE UPDATE, DELETE ON public.contact_requests FROM vietgara_app;
GRANT UPDATE (status) ON public.contact_requests TO vietgara_app;
