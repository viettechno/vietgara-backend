-- Catalogue (HLD module #18) and Inventory (HLD module #7), Database Design
-- Section 5 (FR-CAT-01..04/09, FR-INV-01..03/05). One migration file for both
-- modules because the Inventory tables refer to the Catalogue ones; ownership
-- is by module: Catalogue owns units, part_categories, items (the part),
-- item_units and item_images; Inventory owns stock_balance and
-- stock_transactions. Foreign keys only point towards Catalogue.

CREATE TYPE stock_transaction_type AS ENUM ('in', 'out');
CREATE TYPE stock_transaction_ref_type AS ENUM ('purchase', 'repair_order');

-- ---------------------------------------------------------------------------
-- units: the garage's units of measure (FR-CAT-01). A unit in use is
-- deactivated (is_active = false), never hard-deleted (FR-CAT-07).
-- ---------------------------------------------------------------------------
CREATE TABLE units (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    name VARCHAR(50) NOT NULL,
    abbreviation VARCHAR(20) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT units_garage_id_id_key UNIQUE (garage_id, id)
);

CREATE UNIQUE INDEX idx_units_garage_name_unique ON units (garage_id, lower(name)) WHERE deleted_at IS NULL;

ALTER TABLE units ENABLE ROW LEVEL SECURITY;
ALTER TABLE units FORCE ROW LEVEL SECURITY;

CREATE POLICY units_rls_policy ON units
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY units_rls_policy ON units IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- part_categories: the category tree of at most three levels (FR-CAT-03).
-- The depth is enforced by the use case; parent and child share a garage
-- through the composite foreign key.
-- ---------------------------------------------------------------------------
CREATE TABLE part_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    parent_id UUID,
    name VARCHAR(255) NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT part_categories_garage_id_id_key UNIQUE (garage_id, id),
    CONSTRAINT part_categories_garage_parent_fkey FOREIGN KEY (garage_id, parent_id)
        REFERENCES part_categories (garage_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX idx_part_categories_sibling_name_unique
    ON part_categories (garage_id, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name))
    WHERE deleted_at IS NULL;
CREATE INDEX idx_part_categories_garage_parent ON part_categories (garage_id, parent_id) WHERE deleted_at IS NULL;

ALTER TABLE part_categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE part_categories FORCE ROW LEVEL SECURITY;

CREATE POLICY part_categories_rls_policy ON part_categories
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY part_categories_rls_policy ON part_categories IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- items: the part (FR-INV-01, FR-CAT-01..04/09). Soft-deleted (deleted_at)
-- because stock_transactions and document lines keep referencing them. Stock
-- is always kept in base_unit_id; the quantities live in Inventory.
-- ---------------------------------------------------------------------------
CREATE TABLE items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    sku VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    base_unit_id UUID NOT NULL,
    -- What the customer pays per base unit, whole dong (FR-CAT-09).
    sale_price BIGINT NOT NULL DEFAULT 0 CHECK (sale_price >= 0),
    category_id UUID,
    brand VARCHAR(100) NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    -- In the base unit.
    reorder_threshold NUMERIC(14, 3) NOT NULL DEFAULT 0,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    -- Target of the stock_transactions (garage_id, item_id) foreign keys and
    -- of item_units / item_images.
    CONSTRAINT items_garage_id_id_key UNIQUE (garage_id, id),
    CONSTRAINT items_garage_base_unit_fkey FOREIGN KEY (garage_id, base_unit_id)
        REFERENCES units (garage_id, id) ON DELETE RESTRICT,
    -- Only category_id is set to NULL: a plain SET NULL would null garage_id.
    CONSTRAINT items_garage_category_fkey FOREIGN KEY (garage_id, category_id)
        REFERENCES part_categories (garage_id, id) ON DELETE SET NULL (category_id)
);

CREATE UNIQUE INDEX idx_items_garage_sku_unique ON items (garage_id, sku) WHERE deleted_at IS NULL;
CREATE INDEX idx_items_garage_created_at ON items (garage_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_items_garage_category ON items (garage_id, category_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_items_name_trgm ON items USING gin (name gin_trgm_ops);
CREATE INDEX idx_items_sku_trgm ON items USING gin (sku gin_trgm_ops);
CREATE INDEX idx_items_brand_trgm ON items USING gin (brand gin_trgm_ops);

ALTER TABLE items ENABLE ROW LEVEL SECURITY;
ALTER TABLE items FORCE ROW LEVEL SECURITY;

CREATE POLICY items_rls_policy ON items
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY items_rls_policy ON items IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- item_units: the packaging units of a part with their factor to the base
-- unit (FR-CAT-02): 1 case = 24 cans is factor 24 when the base unit is the
-- can. The base unit has no row. sale_price overrides base price * factor.
-- ---------------------------------------------------------------------------
CREATE TABLE item_units (
    garage_id UUID NOT NULL,
    item_id UUID NOT NULL,
    unit_id UUID NOT NULL,
    factor NUMERIC(18, 6) NOT NULL CHECK (factor > 0),
    sale_price BIGINT CHECK (sale_price IS NULL OR sale_price >= 0),
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (garage_id, item_id, unit_id),
    CONSTRAINT item_units_garage_item_fkey FOREIGN KEY (garage_id, item_id)
        REFERENCES items (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT item_units_garage_unit_fkey FOREIGN KEY (garage_id, unit_id)
        REFERENCES units (garage_id, id) ON DELETE RESTRICT
);

ALTER TABLE item_units ENABLE ROW LEVEL SECURITY;
ALTER TABLE item_units FORCE ROW LEVEL SECURITY;

CREATE POLICY item_units_rls_policy ON item_units
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY item_units_rls_policy ON item_units IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- item_images: the one to three images of a part (FR-CAT-04), files of kind
-- part_image in display order. file_id is checked by the use case (the File
-- module is another module, so there is no foreign key).
-- ---------------------------------------------------------------------------
CREATE TABLE item_images (
    garage_id UUID NOT NULL,
    item_id UUID NOT NULL,
    file_id UUID NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (garage_id, item_id, file_id),
    CONSTRAINT item_images_garage_item_fkey FOREIGN KEY (garage_id, item_id)
        REFERENCES items (garage_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_item_images_item ON item_images (garage_id, item_id, position);

ALTER TABLE item_images ENABLE ROW LEVEL SECURITY;
ALTER TABLE item_images FORCE ROW LEVEL SECURITY;

CREATE POLICY item_images_rls_policy ON item_images
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY item_images_rls_policy ON item_images IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- Quote and repair-order lines keep the packaging unit the user typed
-- (columns added in migrations 000015 and 000016, before units existed).
ALTER TABLE quote_lines ADD CONSTRAINT quote_lines_garage_unit_fkey
    FOREIGN KEY (garage_id, unit_id) REFERENCES units (garage_id, id) ON DELETE RESTRICT;
ALTER TABLE repair_order_lines ADD CONSTRAINT repair_order_lines_garage_unit_fkey
    FOREIGN KEY (garage_id, unit_id) REFERENCES units (garage_id, id) ON DELETE RESTRICT;

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
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID,
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
    -- The packaging unit the user entered and the typed amount (Release 6.0);
    -- NULL when the base unit was used. quantity is always the base amount.
    unit_id UUID,
    entered_quantity NUMERIC(14, 3),
    ref_type stock_transaction_ref_type NOT NULL,
    -- The repair order id for a 'repair_order' issue; NULL for a purchase.
    ref_id UUID,
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT stock_transactions_garage_item_fkey FOREIGN KEY (garage_id, item_id)
        REFERENCES items (garage_id, id),
    CONSTRAINT stock_transactions_garage_unit_fkey FOREIGN KEY (garage_id, unit_id)
        REFERENCES units (garage_id, id) ON DELETE RESTRICT
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
