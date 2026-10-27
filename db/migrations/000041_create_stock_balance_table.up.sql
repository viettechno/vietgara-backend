-- stock_balance: current on-hand quantity per part (FR-INV-03), kept
-- current by an atomic upsert alongside each stock_transactions insert,
-- in the same application transaction (the same pattern as
-- document_sequences.next_seq, migration create_document_sequences_table). quantity_on_hand is
-- updated with in-database arithmetic ("+= delta") for correctness under
-- concurrent receipts/issues of the same part; the Go layer only ever
-- sees it as a decimal string (cast to/from text at the query boundary),
-- matching every other quantity in the API.

CREATE TABLE public.stock_balance (
    garage_id uuid NOT NULL,
    part_id uuid NOT NULL,
    quantity_on_hand numeric(14,3) DEFAULT 0 NOT NULL,
    average_cost numeric(18,4) DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    created_by uuid,
    updated_by uuid,
    CONSTRAINT stock_balance_average_cost_check CHECK ((average_cost >= (0)::numeric))
);

ALTER TABLE ONLY public.stock_balance FORCE ROW LEVEL SECURITY;

ALTER TABLE ONLY public.stock_balance
    ADD CONSTRAINT stock_balance_pkey PRIMARY KEY (garage_id, part_id);

ALTER TABLE ONLY public.stock_balance
    ADD CONSTRAINT stock_balance_garage_part_fkey FOREIGN KEY (garage_id, part_id) REFERENCES public.parts(garage_id, id) ON DELETE CASCADE;

CREATE TRIGGER trigger_stock_balance_row_audit BEFORE INSERT OR UPDATE ON public.stock_balance FOR EACH ROW EXECUTE FUNCTION public.set_row_audit();

ALTER TABLE public.stock_balance ENABLE ROW LEVEL SECURITY;

CREATE POLICY stock_balance_rls_policy ON public.stock_balance USING ((garage_id = public.app_current_garage_id())) WITH CHECK ((garage_id = public.app_current_garage_id()));

COMMENT ON POLICY stock_balance_rls_policy ON public.stock_balance IS 'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
