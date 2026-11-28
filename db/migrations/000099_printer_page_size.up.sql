-- Document/Print (Release 13.0): an IPP printer stores the page size loaded
-- in it, A4 or A5, so that a garage with an A4 and an A5 printer prints each
-- with its own templates. NULL keeps printing on the garage's setting.

ALTER TABLE public.garage_printers DROP CONSTRAINT garage_printers_paper_size_check;

ALTER TABLE public.garage_printers
    ADD CONSTRAINT garage_printers_paper_size_check CHECK (((((protocol)::text = 'raw'::text) AND (paper_size IS NOT NULL) AND ((paper_size)::text = ANY ((ARRAY['K80'::character varying, 'K57'::character varying, 'L5030'::character varying])::text[]))) OR (((protocol)::text = 'ipp'::text) AND ((paper_size IS NULL) OR ((paper_size)::text = ANY ((ARRAY['A4'::character varying, 'A5'::character varying])::text[])))))) NOT VALID;

ALTER TABLE public.garage_printers VALIDATE CONSTRAINT garage_printers_paper_size_check;
