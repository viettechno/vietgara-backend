-- Rolling back deletes the print log rows of the thermal rolls (K80, K57):
-- the column goes back to two characters. Those prints are then forgotten:
-- the next print of such a document is not marked as a copy.

DROP TABLE IF EXISTS public.garage_printers;

ALTER TABLE public.print_log NO FORCE ROW LEVEL SECURITY;
DELETE FROM public.print_log WHERE paper_size IN ('K80', 'K57');
ALTER TABLE public.print_log FORCE ROW LEVEL SECURITY;

ALTER TABLE public.print_log DROP CONSTRAINT print_log_paper_size_check;

ALTER TABLE public.print_log ALTER COLUMN paper_size TYPE character varying(2);

ALTER TABLE public.print_log
    ADD CONSTRAINT print_log_paper_size_check CHECK (((paper_size)::text = ANY ((ARRAY['A4'::character varying, 'A5'::character varying])::text[]))) NOT VALID;

ALTER TABLE public.print_log VALIDATE CONSTRAINT print_log_paper_size_check;
