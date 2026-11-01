DROP INDEX IF EXISTS public.idx_credit_ledger_user_seq;
ALTER TABLE public.credit_ledger DROP COLUMN IF EXISTS seq;
