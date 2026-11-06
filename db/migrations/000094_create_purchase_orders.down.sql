DROP INDEX IF EXISTS public.idx_goods_receipts_purchase_order;

ALTER TABLE public.goods_receipts
    DROP CONSTRAINT IF EXISTS goods_receipts_garage_purchase_order_fkey,
    DROP COLUMN IF EXISTS purchase_order_id;

DROP TABLE IF EXISTS public.purchase_order_lines;
DROP TABLE IF EXISTS public.purchase_orders;
DROP TYPE IF EXISTS public.purchase_order_status;
