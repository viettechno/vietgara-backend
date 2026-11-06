DROP INDEX IF EXISTS public.idx_repair_order_lines_completed;

ALTER TABLE public.repair_order_lines
    DROP COLUMN IF EXISTS completed_at,
    DROP COLUMN IF EXISTS started_at;
