-- The entries of one approval share a created_at (it is the transaction's
-- time), so the ledger could not say which came last. seq orders them in the
-- order they were written.
ALTER TABLE public.credit_ledger ADD COLUMN seq bigint GENERATED ALWAYS AS IDENTITY;

CREATE INDEX idx_credit_ledger_user_seq ON public.credit_ledger USING btree (user_id, seq DESC);
