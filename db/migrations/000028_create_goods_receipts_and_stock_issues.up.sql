-- Inventory, Release 7.0 (FR-INV-06/07/19/20): goods receipt documents, the
-- usual suppliers of a part, and the stock issue document of a repair order.
-- All five tables are owned by Inventory; foreign keys only point towards
-- Catalogue (parts, units) and Supplier. The repair order is referred to by
-- id only (another module, no foreign key).

CREATE TYPE goods_receipt_status AS ENUM ('draft', 'completed', 'cancelled');
CREATE TYPE stock_issue_status AS ENUM ('draft', 'posted', 'cancelled');

-- ---------------------------------------------------------------------------
-- goods_receipts: a numbered multi-line delivery document (FR-INV-06). The
-- header supplier is a default that new lines inherit; every line carries its
-- own supplier. Completing posts the stock movements and updates the average
-- cost; cancelling a completed receipt reverses them.
-- ---------------------------------------------------------------------------
CREATE TABLE goods_receipts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    number VARCHAR(50) NOT NULL,
    status goods_receipt_status NOT NULL DEFAULT 'draft',
    default_supplier_id UUID,
    -- In the garage's timezone.
    receipt_date DATE NOT NULL,
    -- The supplier's own delivery note number.
    delivery_note_number VARCHAR(100) NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    completed_at TIMESTAMP WITH TIME ZONE,
    cancelled_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT goods_receipts_garage_id_id_key UNIQUE (garage_id, id),
    CONSTRAINT goods_receipts_garage_default_supplier_fkey FOREIGN KEY (garage_id, default_supplier_id)
        REFERENCES suppliers (garage_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX idx_goods_receipts_garage_number_unique ON goods_receipts (garage_id, number);
CREATE INDEX idx_goods_receipts_garage_created_at ON goods_receipts (garage_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- goods_receipt_lines: one part of a receipt with its supplier. entered_*
-- is what the user typed (in unit_id, NULL for the base unit); quantity is
-- the derived base-unit amount. unit_cost is VND per entered unit as typed;
-- 0 means "not recorded".
-- ---------------------------------------------------------------------------
CREATE TABLE goods_receipt_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    goods_receipt_id UUID NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    part_id UUID NOT NULL,
    unit_id UUID,
    entered_quantity NUMERIC(14, 3) NOT NULL CHECK (entered_quantity > 0),
    quantity NUMERIC(14, 3) NOT NULL CHECK (quantity > 0),
    supplier_id UUID,
    unit_cost BIGINT NOT NULL DEFAULT 0 CHECK (unit_cost >= 0),
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT goods_receipt_lines_garage_receipt_fkey FOREIGN KEY (garage_id, goods_receipt_id)
        REFERENCES goods_receipts (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT goods_receipt_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id)
        REFERENCES parts (garage_id, id),
    CONSTRAINT goods_receipt_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id)
        REFERENCES units (garage_id, id) ON DELETE RESTRICT,
    CONSTRAINT goods_receipt_lines_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id)
        REFERENCES suppliers (garage_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_goods_receipt_lines_receipt ON goods_receipt_lines (garage_id, goods_receipt_id, position);
CREATE INDEX idx_goods_receipt_lines_supplier ON goods_receipt_lines (garage_id, supplier_id);
CREATE INDEX idx_goods_receipt_lines_part ON goods_receipt_lines (garage_id, part_id);

-- ---------------------------------------------------------------------------
-- part_suppliers: the usual suppliers of a part (FR-INV-19) with the
-- supplier's own code and the last cost paid per base unit; at most one is
-- the default that pre-fills a receipt line.
-- ---------------------------------------------------------------------------
CREATE TABLE part_suppliers (
    garage_id UUID NOT NULL,
    part_id UUID NOT NULL,
    supplier_id UUID NOT NULL,
    supplier_sku VARCHAR(100) NOT NULL DEFAULT '',
    last_unit_cost NUMERIC(18, 4),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (garage_id, part_id, supplier_id),
    CONSTRAINT part_suppliers_garage_part_fkey FOREIGN KEY (garage_id, part_id)
        REFERENCES parts (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT part_suppliers_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id)
        REFERENCES suppliers (garage_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX idx_part_suppliers_default_unique ON part_suppliers (garage_id, part_id) WHERE is_default;

-- ---------------------------------------------------------------------------
-- stock_issues: the numbered stock-out document of a repair order
-- (FR-INV-20), one per order. A draft follows the order's part lines and is
-- the reservation; posting writes the stock movements.
-- ---------------------------------------------------------------------------
CREATE TABLE stock_issues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    number VARCHAR(50) NOT NULL,
    repair_order_id UUID NOT NULL,
    -- The repair order's number at creation, for display and printing.
    repair_order_number VARCHAR(50) NOT NULL DEFAULT '',
    status stock_issue_status NOT NULL DEFAULT 'draft',
    posted_at TIMESTAMP WITH TIME ZONE,
    posted_by UUID,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT stock_issues_garage_id_id_key UNIQUE (garage_id, id)
);

CREATE UNIQUE INDEX idx_stock_issues_garage_number_unique ON stock_issues (garage_id, number);
CREATE UNIQUE INDEX idx_stock_issues_garage_repair_order_unique ON stock_issues (garage_id, repair_order_id);

-- unit_cost is the average-cost snapshot (VND per base unit) taken when the
-- issue is posted; NULL while a draft or when no cost was known.
CREATE TABLE stock_issue_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    stock_issue_id UUID NOT NULL,
    repair_order_line_id UUID NOT NULL,
    part_id UUID NOT NULL,
    unit_id UUID,
    entered_quantity NUMERIC(14, 3) NOT NULL,
    quantity NUMERIC(14, 3) NOT NULL CHECK (quantity > 0),
    unit_cost NUMERIC(18, 4),
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT stock_issue_lines_garage_issue_fkey FOREIGN KEY (garage_id, stock_issue_id)
        REFERENCES stock_issues (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT stock_issue_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id)
        REFERENCES parts (garage_id, id),
    CONSTRAINT stock_issue_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id)
        REFERENCES units (garage_id, id) ON DELETE RESTRICT,
    CONSTRAINT stock_issue_lines_issue_line_unique UNIQUE (stock_issue_id, repair_order_line_id)
);

CREATE INDEX idx_stock_issue_lines_issue ON stock_issue_lines (garage_id, stock_issue_id);
-- The reserved quantity of a part sums the lines of draft issues.
CREATE INDEX idx_stock_issue_lines_part ON stock_issue_lines (garage_id, part_id);

-- Row-Level Security, policies and audit triggers.
DO $$
DECLARE
    t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['goods_receipts', 'goods_receipt_lines', 'part_suppliers', 'stock_issues', 'stock_issue_lines']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY %I ON %I FOR ALL USING (garage_id = app_current_garage_id()) WITH CHECK (garage_id = app_current_garage_id())',
            t || '_rls_policy', t);
        EXECUTE format(
            'COMMENT ON POLICY %I ON %I IS %L', t || '_rls_policy', t,
            'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).');
        EXECUTE format(
            'CREATE TRIGGER %I BEFORE INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION set_row_audit()',
            'trigger_' || t || '_row_audit', t);
    END LOOP;
END $$;
