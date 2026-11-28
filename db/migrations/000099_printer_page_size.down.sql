-- Rolling back forgets the page size of the IPP printers: they print on the
-- garage's setting again, with the documents they listed (an A5 printer's
-- quotes then print on the quote's setting, maybe A4 on A5 paper: set the
-- garage's setting to match before rolling back).

ALTER TABLE public.garage_printers NO FORCE ROW LEVEL SECURITY;
UPDATE public.garage_printers SET paper_size = NULL WHERE protocol = 'ipp';
ALTER TABLE public.garage_printers FORCE ROW LEVEL SECURITY;

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_paper_size_check;

ALTER TABLE public.garage_printers
    ADD CONSTRAINT garage_printers_paper_size_check CHECK (((((protocol)::text = 'raw'::text) AND (paper_size IS NOT NULL) AND ((paper_size)::text = ANY ((ARRAY['K80'::character varying, 'K57'::character varying, 'L5030'::character varying])::text[]))) OR (((protocol)::text = 'ipp'::text) AND (paper_size IS NULL)))) NOT VALID;

ALTER TABLE public.garage_printers VALIDATE CONSTRAINT garage_printers_paper_size_check;
