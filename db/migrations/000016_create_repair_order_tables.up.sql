-- Repair Order module (HLD module #6), Database Design Section 4.3-4.4.
-- Release 1.2 only needs the draft created by Quote.ConvertQuote
-- (FR-QUO-04); technician assignment, the Kanban board, progress per line
-- and photos are Release 1.3 and will extend this table then.

CREATE TYPE repair_order_status AS ENUM ('open', 'in_progress', 'completed', 'closed');

CREATE TABLE repair_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    -- The quote it was converted from. Nullable for Release 1.3's ad-hoc
    -- creation (FR-REP-01); always set today.
    quote_id UUID,
    vehicle_id UUID NOT NULL,
    -- Rendered from the garage's repair_order_number_template (FR-TEN-05).
    number VARCHAR(50) NOT NULL,
    status repair_order_status NOT NULL DEFAULT 'open',
    -- Sum of repair_order_lines.line_total at creation time.
    total_amount BIGINT NOT NULL DEFAULT 0,
    closed_at TIMESTAMP WITH TIME ZONE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT repair_orders_garage_quote_fkey FOREIGN KEY (garage_id, quote_id)
        REFERENCES quotes (garage_id, id),
    CONSTRAINT repair_orders_garage_vehicle_fkey FOREIGN KEY (garage_id, vehicle_id)
        REFERENCES vehicles (garage_id, id),
    -- Target of the repair_order_lines (garage_id, repair_order_id) foreign key.
    CONSTRAINT repair_orders_garage_id_id_key UNIQUE (garage_id, id),
    -- A quote converts into at most one repair order.
    CONSTRAINT repair_orders_quote_id_unique UNIQUE (quote_id)
);

CREATE TRIGGER trigger_repair_orders_updated_at BEFORE UPDATE ON repair_orders FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE UNIQUE INDEX idx_repair_orders_garage_number_unique ON repair_orders (garage_id, number) WHERE deleted_at IS NULL;
CREATE INDEX idx_repair_orders_garage_status_created_at ON repair_orders (garage_id, status, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_repair_orders_vehicle_id ON repair_orders (garage_id, vehicle_id, created_at DESC) WHERE deleted_at IS NULL;

ALTER TABLE repair_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE repair_orders FORCE ROW LEVEL SECURITY;

CREATE POLICY repair_orders_rls_policy ON repair_orders
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY repair_orders_rls_policy ON repair_orders IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';

-- ---------------------------------------------------------------------------
-- repair_order_lines: the approved quote lines carried over by
-- ConvertQuote (FR-QUO-04). Per-line technician assignment and progress
-- (FR-REP-02/03) are added in Release 1.3.
-- ---------------------------------------------------------------------------
CREATE TABLE repair_order_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    repair_order_id UUID NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    item_type quote_item_type NOT NULL,
    item_ref_id UUID,
    description TEXT NOT NULL,
    quantity VARCHAR(12) NOT NULL DEFAULT '1',
    unit_price BIGINT NOT NULL DEFAULT 0,
    line_total BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT repair_order_lines_garage_ro_fkey FOREIGN KEY (garage_id, repair_order_id)
        REFERENCES repair_orders (garage_id, id) ON DELETE CASCADE
);

CREATE TRIGGER trigger_repair_order_lines_updated_at BEFORE UPDATE ON repair_order_lines FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX idx_repair_order_lines_ro_id ON repair_order_lines (garage_id, repair_order_id, position);

ALTER TABLE repair_order_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE repair_order_lines FORCE ROW LEVEL SECURITY;

CREATE POLICY repair_order_lines_rls_policy ON repair_order_lines
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY repair_order_lines_rls_policy ON repair_order_lines IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
