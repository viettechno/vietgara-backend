-- Document/Print (Release 13.0, FR-DOC-06, FR-DOC-07): the print log takes
-- the thermal rolls K80 and K57, and a garage saves its network printers.
-- The server never connects to a printer: the apps on the garage's network
-- do, with what is stored here.

ALTER TABLE public.print_log DROP CONSTRAINT print_log_paper_size_check;

ALTER TABLE public.print_log ALTER COLUMN paper_size TYPE character varying(3);

ALTER TABLE public.print_log
    ADD CONSTRAINT print_log_paper_size_check CHECK (((paper_size)::text = ANY ((ARRAY['A4'::character varying, 'A5'::character varying, 'K80'::character varying, 'K57'::character varying])::text[]))) NOT VALID;

ALTER TABLE public.print_log VALIDATE CONSTRAINT print_log_paper_size_check;

CREATE TABLE public.garage_printers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    garage_id uuid NOT NULL,
    name character varying(60) NOT NULL,
    protocol character varying(3) NOT NULL,
    host character varying(253) NOT NULL,
    port integer NOT NULL,
    path character varying(200) DEFAULT ''::character varying NOT NULL,
    paper_size character varying(5),
    document_types text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by uuid,
    CONSTRAINT garage_printers_protocol_check CHECK (((protocol)::text = ANY ((ARRAY['raw'::character varying, 'ipp'::character varying])::text[]))),
    CONSTRAINT garage_printers_port_check CHECK (((port >= 1) AND (port <= 65535))),
    CONSTRAINT garage_printers_paper_size_check CHECK (((((protocol)::text = 'raw'::text) AND ((paper_size)::text = ANY ((ARRAY['K80'::character varying, 'K57'::character varying, 'L5030'::character varying])::text[]))) OR (((protocol)::text = 'ipp'::text) AND (paper_size IS NULL))))
);

ALTER TABLE ONLY public.garage_printers FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.garage_printers
    ADD CONSTRAINT garage_printers_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX idx_garage_printers_name ON public.garage_printers USING btree (garage_id, lower((name)::text));

ALTER TABLE ONLY public.garage_printers
    ADD CONSTRAINT garage_printers_garage_id_fkey FOREIGN KEY (garage_id) REFERENCES public.garages(id) ON DELETE CASCADE;

CREATE TRIGGER trigger_garage_printers_row_audit BEFORE INSERT OR UPDATE ON public.garage_printers FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.garage_printers ENABLE ROW LEVEL SECURITY;

CREATE POLICY garage_printers_rls_policy ON public.garage_printers USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY garage_printers_rls_policy ON public.garage_printers IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
