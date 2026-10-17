-- Inventory, Release 7.1 (FR-INV-09..12): the stock count document, write-offs,
-- returns to supplier and the return of unused parts from a repair order. All
-- eight tables are owned by Inventory; foreign keys only point towards
-- Catalogue (parts, units) and Supplier. The repair order and the goods
-- receipt of a supplier return are referred to by id only.

CREATE TYPE stock_count_status AS ENUM ('draft', 'completed', 'cancelled');
-- Write-offs and supplier returns are posted when created.
CREATE TYPE adjustment_status AS ENUM ('completed', 'cancelled');
CREATE TYPE write_off_reason AS ENUM ('damaged', 'lost', 'expired', 'internal_use', 'warranty_replacement');

-- ---------------------------------------------------------------------------
-- stock_counts: a numbered count of parts (FR-INV-12). A draft is counted
-- line by line; completing it posts the adjustments and freezes it.
-- ---------------------------------------------------------------------------
CREATE TABLE stock_counts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    number VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    status stock_count_status NOT NULL DEFAULT 'draft',
    completed_at TIMESTAMP WITH TIME ZONE,
    completed_by UUID,
    cancelled_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT stock_counts_garage_id_id_key UNIQUE (garage_id, id)
);

CREATE UNIQUE INDEX idx_stock_counts_garage_number_unique ON stock_counts (garage_id, number);
CREATE INDEX idx_stock_counts_garage_created_at ON stock_counts (garage_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- stock_count_lines: one part of a count. system_quantity is the stock on hand
-- (base unit, may be negative) when the line was last counted; counted_* are
-- NULL while the line is uncounted. average_cost is the cost the difference is
-- valued at, written when the count completes (a draft shows the live one).
-- ---------------------------------------------------------------------------
CREATE TABLE stock_count_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    stock_count_id UUID NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    part_id UUID NOT NULL,
    unit_id UUID,
    entered_counted_quantity NUMERIC(14, 3) CHECK (entered_counted_quantity >= 0),
    counted_quantity NUMERIC(14, 3) CHECK (counted_quantity >= 0),
    system_quantity NUMERIC(14, 3) NOT NULL DEFAULT 0,
    average_cost NUMERIC(18, 4),
    counted_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT stock_count_lines_garage_count_fkey FOREIGN KEY (garage_id, stock_count_id)
        REFERENCES stock_counts (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT stock_count_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id)
        REFERENCES parts (garage_id, id),
    CONSTRAINT stock_count_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id)
        REFERENCES units (garage_id, id) ON DELETE RESTRICT,
    CONSTRAINT stock_count_lines_count_part_unique UNIQUE (stock_count_id, part_id),
    CONSTRAINT stock_count_lines_counted_together CHECK ((counted_quantity IS NULL) = (entered_counted_quantity IS NULL))
);

CREATE INDEX idx_stock_count_lines_count ON stock_count_lines (garage_id, stock_count_id, position);
CREATE INDEX idx_stock_count_lines_part ON stock_count_lines (garage_id, part_id);

-- ---------------------------------------------------------------------------
-- write_offs and write_off_lines (FR-INV-09): stock leaves at average cost for
-- a reason; unit_cost is the average-cost snapshot per base unit.
-- ---------------------------------------------------------------------------
CREATE TABLE write_offs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    number VARCHAR(50) NOT NULL,
    status adjustment_status NOT NULL DEFAULT 'completed',
    reason write_off_reason NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    cancelled_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT write_offs_garage_id_id_key UNIQUE (garage_id, id)
);

CREATE UNIQUE INDEX idx_write_offs_garage_number_unique ON write_offs (garage_id, number);
CREATE INDEX idx_write_offs_garage_created_at ON write_offs (garage_id, created_at DESC);

CREATE TABLE write_off_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    write_off_id UUID NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    part_id UUID NOT NULL,
    unit_id UUID,
    entered_quantity NUMERIC(14, 3) NOT NULL CHECK (entered_quantity > 0),
    quantity NUMERIC(14, 3) NOT NULL CHECK (quantity > 0),
    unit_cost NUMERIC(18, 4),
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT write_off_lines_garage_write_off_fkey FOREIGN KEY (garage_id, write_off_id)
        REFERENCES write_offs (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT write_off_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id)
        REFERENCES parts (garage_id, id),
    CONSTRAINT write_off_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id)
        REFERENCES units (garage_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_write_off_lines_write_off ON write_off_lines (garage_id, write_off_id, position);
CREATE INDEX idx_write_off_lines_part ON write_off_lines (garage_id, part_id);

-- ---------------------------------------------------------------------------
-- supplier_returns and supplier_return_lines (FR-INV-10): stock leaves and the
-- supplier owes a credit. unit_cost is the credit per entered unit (VND);
-- average_cost is the stock's average-cost snapshot per base unit, used to
-- put the parts back when the return is cancelled.
-- ---------------------------------------------------------------------------
CREATE TABLE supplier_returns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    number VARCHAR(50) NOT NULL,
    status adjustment_status NOT NULL DEFAULT 'completed',
    supplier_id UUID NOT NULL,
    goods_receipt_id UUID,
    credit_note_number VARCHAR(100) NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    cancelled_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT supplier_returns_garage_id_id_key UNIQUE (garage_id, id),
    CONSTRAINT supplier_returns_garage_supplier_fkey FOREIGN KEY (garage_id, supplier_id)
        REFERENCES suppliers (garage_id, id) ON DELETE RESTRICT,
    CONSTRAINT supplier_returns_garage_receipt_fkey FOREIGN KEY (garage_id, goods_receipt_id)
        REFERENCES goods_receipts (garage_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX idx_supplier_returns_garage_number_unique ON supplier_returns (garage_id, number);
CREATE INDEX idx_supplier_returns_garage_created_at ON supplier_returns (garage_id, created_at DESC);
CREATE INDEX idx_supplier_returns_supplier ON supplier_returns (garage_id, supplier_id);

CREATE TABLE supplier_return_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    supplier_return_id UUID NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    part_id UUID NOT NULL,
    unit_id UUID,
    entered_quantity NUMERIC(14, 3) NOT NULL CHECK (entered_quantity > 0),
    quantity NUMERIC(14, 3) NOT NULL CHECK (quantity > 0),
    unit_cost BIGINT NOT NULL DEFAULT 0 CHECK (unit_cost >= 0),
    average_cost NUMERIC(18, 4),
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT supplier_return_lines_garage_return_fkey FOREIGN KEY (garage_id, supplier_return_id)
        REFERENCES supplier_returns (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT supplier_return_lines_garage_part_fkey FOREIGN KEY (garage_id, part_id)
        REFERENCES parts (garage_id, id),
    CONSTRAINT supplier_return_lines_garage_unit_fkey FOREIGN KEY (garage_id, unit_id)
        REFERENCES units (garage_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_supplier_return_lines_return ON supplier_return_lines (garage_id, supplier_return_id, position);
CREATE INDEX idx_supplier_return_lines_part ON supplier_return_lines (garage_id, part_id);

-- ---------------------------------------------------------------------------
-- repair_order_part_returns (FR-INV-11): unused parts coming back from a
-- repair order, immutable. unit_cost is the cost snapshot of the stock issue
-- line they are restored at (base unit).
-- ---------------------------------------------------------------------------
CREATE TABLE repair_order_part_returns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    repair_order_id UUID NOT NULL,
    repair_order_number VARCHAR(50) NOT NULL DEFAULT '',
    stock_issue_id UUID NOT NULL,
    stock_issue_line_id UUID NOT NULL,
    repair_order_line_id UUID NOT NULL,
    part_id UUID NOT NULL,
    unit_id UUID,
    entered_quantity NUMERIC(14, 3) NOT NULL CHECK (entered_quantity > 0),
    quantity NUMERIC(14, 3) NOT NULL CHECK (quantity > 0),
    reason TEXT NOT NULL,
    unit_cost NUMERIC(18, 4),
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT repair_order_part_returns_garage_issue_fkey FOREIGN KEY (garage_id, stock_issue_id)
        REFERENCES stock_issues (garage_id, id) ON DELETE RESTRICT,
    CONSTRAINT repair_order_part_returns_issue_line_fkey FOREIGN KEY (stock_issue_line_id)
        REFERENCES stock_issue_lines (id) ON DELETE RESTRICT,
    CONSTRAINT repair_order_part_returns_garage_part_fkey FOREIGN KEY (garage_id, part_id)
        REFERENCES parts (garage_id, id),
    CONSTRAINT repair_order_part_returns_garage_unit_fkey FOREIGN KEY (garage_id, unit_id)
        REFERENCES units (garage_id, id) ON DELETE RESTRICT
);

CREATE INDEX idx_repair_order_part_returns_order ON repair_order_part_returns (garage_id, repair_order_id, created_at);
CREATE INDEX idx_repair_order_part_returns_line ON repair_order_part_returns (stock_issue_line_id);
CREATE INDEX idx_repair_order_part_returns_part ON repair_order_part_returns (garage_id, part_id);

-- Row-Level Security, policies and audit triggers.
DO $$
DECLARE
    t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'stock_counts', 'stock_count_lines', 'write_offs', 'write_off_lines', 'supplier_returns', 'supplier_return_lines',
        'repair_order_part_returns'
    ]
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
