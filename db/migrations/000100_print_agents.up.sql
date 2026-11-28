-- Document/Print (Release 13.1, FR-DOC-08): print agents. An agent is a small
-- program on a garage PC that keeps an outbound WebSocket to VietGara and
-- prints the jobs sent to the PC's printers. It is paired once with a
-- short-lived pairing code and then authenticates with its own key; only the
-- SHA-256 hashes of the code and of the key are stored.

CREATE TABLE public.print_agents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    name character varying(60) NOT NULL,
    key_hash bytea,
    pairing_code_hash bytea,
    pairing_expires_at timestamp with time zone,
    paired_at timestamp with time zone,
    hostname character varying(253) DEFAULT ''::character varying NOT NULL,
    os character varying(40) DEFAULT ''::character varying NOT NULL,
    version character varying(40) DEFAULT ''::character varying NOT NULL,
    printers jsonb DEFAULT '[]'::jsonb NOT NULL,
    -- The connection serving the agent (one backend instance holds it) and
    -- its heartbeat: the agent is connected while both are set and recent.
    connection_id uuid,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid,
    CONSTRAINT print_agents_pairing_check CHECK (((pairing_code_hash IS NULL) = (pairing_expires_at IS NULL)))
);

ALTER TABLE ONLY public.print_agents FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.print_agents
    ADD CONSTRAINT print_agents_pkey PRIMARY KEY (id);

-- The target of the garage-matching foreign keys of the printers and jobs: a
-- printer or job can only name an agent of its own garage.
ALTER TABLE ONLY public.print_agents
    ADD CONSTRAINT print_agents_garage_id_id_key UNIQUE (garage_id, id);

CREATE UNIQUE INDEX idx_print_agents_name ON public.print_agents USING btree (garage_id, lower((name)::text));

CREATE UNIQUE INDEX idx_print_agents_key_hash ON public.print_agents USING btree (key_hash) WHERE (key_hash IS NOT NULL);

CREATE UNIQUE INDEX idx_print_agents_pairing_code_hash ON public.print_agents USING btree (pairing_code_hash) WHERE (pairing_code_hash IS NOT NULL);

ALTER TABLE ONLY public.print_agents
    ADD CONSTRAINT print_agents_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_print_agents_row_audit BEFORE INSERT OR UPDATE ON public.print_agents FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.print_agents ENABLE ROW LEVEL SECURITY;

CREATE POLICY print_agents_rls_policy ON public.print_agents USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY print_agents_rls_policy ON public.print_agents IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- The agent's own calls come before any garage is known: these find the
-- agent of a pairing code (still valid) or of a key, like
-- find_quote_by_public_token.
CREATE FUNCTION public.find_print_agent_by_pairing_code(code_hash bytea) RETURNS TABLE(id uuid, garage_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public'
    AS $$
    SELECT a.id, a.garage_id FROM print_agents a
    WHERE a.pairing_code_hash = code_hash
      AND a.pairing_expires_at > now()
$$;

GRANT ALL ON FUNCTION public.find_print_agent_by_pairing_code(code_hash bytea) TO vietgara_app;

CREATE FUNCTION public.find_print_agent_by_key(agent_key_hash bytea) RETURNS TABLE(id uuid, garage_id uuid)
    LANGUAGE sql STABLE SECURITY DEFINER
    SET search_path TO 'public'
    AS $$
    SELECT a.id, a.garage_id FROM print_agents a
    WHERE a.key_hash = agent_key_hash
$$;

GRANT ALL ON FUNCTION public.find_print_agent_by_key(agent_key_hash bytea) TO vietgara_app;

-- A printer of an agent: the PC's printer (its name in the operating
-- system, "queue"), the paper loaded in it and the documents it prints first.
ALTER TABLE public.garage_printers
    ADD COLUMN agent_id uuid,
    ADD COLUMN queue character varying(200) DEFAULT ''::character varying NOT NULL;

ALTER TABLE ONLY public.garage_printers
    ADD CONSTRAINT garage_printers_agent_fkey FOREIGN KEY (garage_id, agent_id) REFERENCES public.print_agents(garage_id, id) ON DELETE CASCADE;

CREATE INDEX idx_garage_printers_agent ON public.garage_printers USING btree (agent_id) WHERE (agent_id IS NOT NULL);

CREATE UNIQUE INDEX idx_garage_printers_agent_queue ON public.garage_printers USING btree (agent_id, lower((queue)::text)) WHERE (agent_id IS NOT NULL);

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_protocol_check;

ALTER TABLE public.garage_printers ALTER COLUMN protocol TYPE character varying(5);

ALTER TABLE public.garage_printers
    ADD CONSTRAINT garage_printers_protocol_check CHECK (((protocol)::text = ANY ((ARRAY['raw'::character varying, 'ipp'::character varying, 'agent'::character varying])::text[]))) NOT VALID;

ALTER TABLE public.garage_printers VALIDATE CONSTRAINT garage_printers_protocol_check;

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_port_check;

ALTER TABLE public.garage_printers
    ADD CONSTRAINT garage_printers_port_check CHECK (((((protocol)::text = 'agent'::text) AND (port = 0)) OR (((protocol)::text <> 'agent'::text) AND (port >= 1) AND (port <= 65535)))) NOT VALID;

ALTER TABLE public.garage_printers VALIDATE CONSTRAINT garage_printers_port_check;

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_paper_size_check;

ALTER TABLE public.garage_printers
    ADD CONSTRAINT garage_printers_paper_size_check CHECK (((((protocol)::text = 'raw'::text) AND (paper_size IS NOT NULL) AND ((paper_size)::text = ANY ((ARRAY['K80'::character varying, 'K57'::character varying, 'L5030'::character varying])::text[]))) OR (((protocol)::text = 'ipp'::text) AND ((paper_size IS NULL) OR ((paper_size)::text = ANY ((ARRAY['A4'::character varying, 'A5'::character varying])::text[])))) OR (((protocol)::text = 'agent'::text) AND ((paper_size IS NULL) OR ((paper_size)::text = ANY ((ARRAY['K80'::character varying, 'K57'::character varying, 'L5030'::character varying, 'A4'::character varying, 'A5'::character varying])::text[])))))) NOT VALID;

ALTER TABLE public.garage_printers VALIDATE CONSTRAINT garage_printers_paper_size_check;

ALTER TABLE public.garage_printers
    ADD CONSTRAINT garage_printers_agent_check CHECK ((((protocol)::text = 'agent'::text) = (agent_id IS NOT NULL)) AND (((protocol)::text = 'agent'::text) = ((queue)::text <> ''::text)) AND (((protocol)::text <> 'agent'::text) OR ((host)::text = ''::text) AND ((path)::text = ''::text)));

-- A print sent to an agent's printer: the server renders it when it is asked
-- for and keeps the bytes until the agent says how it went (then they are
-- cleared); a job not printed within an hour expires.
CREATE TABLE public.print_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    printer_id uuid,
    printer_name character varying(60) NOT NULL,
    queue character varying(200) NOT NULL,
    document_type character varying(32),
    format character varying(6) NOT NULL,
    paper_size character varying(5),
    data bytea NOT NULL,
    status character varying(8) DEFAULT 'queued'::character varying NOT NULL,
    error character varying(500) DEFAULT ''::character varying NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    sent_at timestamp with time zone,
    finished_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid,
    CONSTRAINT print_jobs_format_check CHECK (((format)::text = ANY ((ARRAY['pdf'::character varying, 'escpos'::character varying])::text[]))),
    CONSTRAINT print_jobs_status_check CHECK (((status)::text = ANY ((ARRAY['queued'::character varying, 'sent'::character varying, 'printed'::character varying, 'failed'::character varying, 'expired'::character varying])::text[])))
);

ALTER TABLE ONLY public.print_jobs FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.print_jobs
    ADD CONSTRAINT print_jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.print_jobs
    ADD CONSTRAINT print_jobs_agent_fkey FOREIGN KEY (garage_id, agent_id) REFERENCES public.print_agents(garage_id, id) ON DELETE CASCADE;

ALTER TABLE ONLY public.print_jobs
    ADD CONSTRAINT print_jobs_printer_id_fkey FOREIGN KEY (printer_id) REFERENCES public.garage_printers(id) ON DELETE SET NULL;

CREATE INDEX idx_print_jobs_pending ON public.print_jobs USING btree (agent_id, created_at) WHERE ((status)::text = ANY ((ARRAY['queued'::character varying, 'sent'::character varying])::text[]));

CREATE INDEX idx_print_jobs_garage_created ON public.print_jobs USING btree (garage_id, created_at);

CREATE INDEX idx_print_jobs_printer ON public.print_jobs USING btree (printer_id) WHERE (printer_id IS NOT NULL);

CREATE TRIGGER trigger_print_jobs_row_audit BEFORE INSERT OR UPDATE ON public.print_jobs FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.print_jobs ENABLE ROW LEVEL SECURITY;

CREATE POLICY print_jobs_rls_policy ON public.print_jobs USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY print_jobs_rls_policy ON public.print_jobs IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
