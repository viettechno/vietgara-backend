-- When a repair order line was started and completed (Release 11.1, FR-RPT-02):
-- started_at is set when the line first leaves 'not_started', completed_at when
-- it becomes 'completed' and cleared if it is reopened. Lines completed before
-- this release count as completed at their last update and have no start, so
-- they have no turnaround.

ALTER TABLE public.repair_order_lines
    ADD COLUMN started_at timestamp with time zone,
    ADD COLUMN completed_at timestamp with time zone;

-- The table forces row-level security, which would hide every row from this
-- statement: lift it for the backfill only.
ALTER TABLE public.repair_order_lines NO FORCE ROW LEVEL SECURITY;

UPDATE public.repair_order_lines SET completed_at = updated_at WHERE status = 'completed';

ALTER TABLE public.repair_order_lines FORCE ROW LEVEL SECURITY;

CREATE INDEX idx_repair_order_lines_completed ON public.repair_order_lines USING btree (garage_id, completed_at) WHERE (completed_at IS NOT NULL);
