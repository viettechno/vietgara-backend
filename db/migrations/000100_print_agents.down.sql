-- Rolling back removes the print agents, their printers and their print
-- jobs: pair the agents again after upgrading once more.

DROP TABLE public.print_jobs;

ALTER TABLE public.garage_printers NO FORCE ROW LEVEL SECURITY;
DELETE FROM public.garage_printers WHERE protocol = 'agent';
ALTER TABLE public.garage_printers FORCE ROW LEVEL SECURITY;

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_agent_check;

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_paper_size_check;

ALTER TABLE public.garage_printers
    ADD CONSTRAINT garage_printers_paper_size_check CHECK (((((protocol)::text = 'raw'::text) AND (paper_size IS NOT NULL) AND ((paper_size)::text = ANY ((ARRAY['K80'::character varying, 'K57'::character varying, 'L5030'::character varying])::text[]))) OR (((protocol)::text = 'ipp'::text) AND ((paper_size IS NULL) OR ((paper_size)::text = ANY ((ARRAY['A4'::character varying, 'A5'::character varying])::text[])))))) NOT VALID;

ALTER TABLE public.garage_printers VALIDATE CONSTRAINT garage_printers_paper_size_check;

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_port_check;

ALTER TABLE public.garage_printers
    ADD CONSTRAINT garage_printers_port_check CHECK (((port >= 1) AND (port <= 65535)));

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_protocol_check;

ALTER TABLE public.garage_printers ALTER COLUMN protocol TYPE character varying(3);

ALTER TABLE public.garage_printers
    ADD CONSTRAINT garage_printers_protocol_check CHECK (((protocol)::text = ANY ((ARRAY['raw'::character varying, 'ipp'::character varying])::text[])));

DROP INDEX public.idx_garage_printers_agent_queue;

DROP INDEX public.idx_garage_printers_agent;

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_agent_fkey;

ALTER TABLE public.garage_printers
    DROP COLUMN queue,
    DROP COLUMN agent_id;

DROP FUNCTION public.find_print_agent_by_key(bytea);

DROP FUNCTION public.find_print_agent_by_pairing_code(bytea);

DROP TABLE public.print_agents;
