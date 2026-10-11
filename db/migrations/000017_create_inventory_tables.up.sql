-- Inventory module (HLD module #7), Database Design Section 5 (FR-INV-01..03/05).

CREATE TYPE stock_transaction_type AS ENUM ('in', 'out');
CREATE TYPE stock_transaction_ref_type AS ENUM ('purchase', 'repair_order');

-- ---------------------------------------------------------------------------
-- items: one part code a garage stocks. Soft-deleted (deleted_at) because
-- stock_transactions and repair_order_lines keep referencing them.
-- ---------------------------------------------------------------------------
CREATE TABLE items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    sku VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    unit VARCHAR(20) NOT NULL DEFAULT '',
    reorder_threshold NUMERIC(14, 3) NOT NULL DEFAULT 0,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    -- Target of the stock_transactions/repair_order_lines (garage_id, item_id)
    -- foreign keys.
    CONSTRAINT items_garage_id_id_key UNIQUE (garage_id, id)
);

CREATE TRIGGER trigger_items_updated_at BEFORE UPDATE ON items FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE UNIQUE INDEX idx_items_garage_sku_unique ON items (garage_id, sku) WHERE deleted_at IS NULL;
CREATE INDEX idx_items_garage_created_at ON items (garage_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_items_name_trgm ON items USING gin (name gin_trgm_ops);
CREATE INDEX idx_items_sku_trgm ON items USING gin (sku gin_trgm_ops);

ALTER TABLE items ENABLE ROW LEVEL SECURITY;
ALTER TABLE items FORCE ROW LEVEL SECURITY;

CREATE POLICY items_rls_policy ON items
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY items_rls_policy ON items IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- stock_balance: current on-hand quantity per item (FR-INV-03), kept
-- current by an atomic upsert alongside each stock_transactions insert,
-- in the same application transaction (the same pattern as
-- document_sequences.next_seq, migration 000014). quantity_on_hand is
-- updated with in-database arithmetic ("+= delta") for correctness under
-- concurrent receipts/issues of the same item; the Go layer only ever
-- sees it as a decimal string (cast to/from text at the query boundary),
-- matching every other quantity in the API.
-- ---------------------------------------------------------------------------
CREATE TABLE stock_balance (
    garage_id UUID NOT NULL,
    item_id UUID NOT NULL,
    quantity_on_hand NUMERIC(14, 3) NOT NULL DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (garage_id, item_id),
    CONSTRAINT stock_balance_garage_item_fkey FOREIGN KEY (garage_id, item_id)
        REFERENCES items (garage_id, id) ON DELETE CASCADE
);

ALTER TABLE stock_balance ENABLE ROW LEVEL SECURITY;
ALTER TABLE stock_balance FORCE ROW LEVEL SECURITY;

CREATE POLICY stock_balance_rls_policy ON stock_balance
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY stock_balance_rls_policy ON stock_balance IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- stock_transactions: an append-only log of every stock movement
-- (FR-INV-01/02/05). quantity is always positive; type gives the
-- direction. quantity is NUMERIC(14,3) like every other quantity, so the
-- valuation and profit reports can aggregate it in SQL.
-- ---------------------------------------------------------------------------
CREATE TABLE stock_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    item_id UUID NOT NULL,
    type stock_transaction_type NOT NULL,
    quantity NUMERIC(14, 3) NOT NULL,
    -- VND; set for a 'purchase' receipt, NULL for a 'repair_order' issue.
    -- Four decimals: a cost rounded to whole dong would drift once average
    -- cost arrives (Phase 7).
    unit_cost NUMERIC(18, 4),
    ref_type stock_transaction_ref_type NOT NULL,
    -- The repair order id for a 'repair_order' issue; NULL for a purchase.
    ref_id UUID,
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT stock_transactions_garage_item_fkey FOREIGN KEY (garage_id, item_id)
        REFERENCES items (garage_id, id)
);

CREATE INDEX idx_stock_transactions_garage_item_created_at ON stock_transactions (garage_id, item_id, created_at DESC);
CREATE INDEX idx_stock_transactions_garage_type_created_at ON stock_transactions (garage_id, type, created_at DESC);
-- Idempotency guard: the repair_order.closed consumer processes each part
-- line's issue at most once per (item, repair order), on top of the
-- generic processed_events check -- see internal/inventory's consumer.
CREATE UNIQUE INDEX idx_stock_transactions_repair_order_item_unique
    ON stock_transactions (garage_id, item_id, ref_id)
    WHERE ref_type = 'repair_order';

ALTER TABLE stock_transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE stock_transactions FORCE ROW LEVEL SECURITY;

CREATE POLICY stock_transactions_rls_policy ON stock_transactions
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY stock_transactions_rls_policy ON stock_transactions IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
