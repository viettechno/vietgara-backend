-- Repair Order module (HLD module #6), Database Design Section 4.3-4.4.
-- The draft created by Quote.ConvertQuote (FR-QUO-04, Release 1.2) and,
-- from Release 1.3, ad-hoc creation, technician assignment, the Kanban
-- board, per-line progress and closing (FR-REP-01..03/06). Before/after
-- photos (FR-REP-04) remain Phase 3.

-- 'waiting_on_parts' is the Kanban board's fourth column (UI Design Spec
-- Section 3.3): a repair order blocked on a part not currently in stock.
CREATE TYPE repair_order_status AS ENUM ('open', 'in_progress', 'waiting_on_parts', 'completed', 'closed');

CREATE TABLE repair_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    -- The quote it was converted from. Nullable for Release 1.3's ad-hoc
    -- creation (FR-REP-01); always set today.
    quote_id UUID,
    vehicle_id UUID NOT NULL,
    -- The vehicle's owner and plate at creation time, denormalized so
    -- this module's queries never join the Customer module's tables
    -- directly (each module owns its own tables, HLD Section 2) -- the
    -- same pattern quotes.customer_name/vehicle_license_plate already use.
    customer_name VARCHAR(255) NOT NULL,
    vehicle_license_plate VARCHAR(20) NOT NULL,
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

-- Progress of one repair order line (FR-REP-03), independent of the
-- repair order's own status (repair_order_status above).
CREATE TYPE repair_order_line_status AS ENUM ('not_started', 'in_progress', 'completed');

-- ---------------------------------------------------------------------------
-- repair_order_lines: the approved quote lines carried over by
-- ConvertQuote (FR-QUO-04), or entered directly for an ad-hoc repair
-- order (FR-REP-01, Release 1.3). item_ref_id references an Inventory
-- item for a part line (Release 1.3); left NULL for a part not carried
-- in stock -- it is then simply skipped when stock is deducted on close
-- (Event Design, repair_order.closed).
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
    status repair_order_line_status NOT NULL DEFAULT 'not_started',
    -- Technician account ids assigned to this line (FR-REP-02); a garage
    -- membership is not enforced at the database level, the same way
    -- garage_staff.account_id elsewhere is not FK-constrained to a
    -- specific garage's roster.
    assigned_technicians UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT repair_order_lines_garage_ro_fkey FOREIGN KEY (garage_id, repair_order_id)
        REFERENCES repair_orders (garage_id, id) ON DELETE CASCADE
);

CREATE TRIGGER trigger_repair_order_lines_updated_at BEFORE UPDATE ON repair_order_lines FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE INDEX idx_repair_order_lines_ro_id ON repair_order_lines (garage_id, repair_order_id, position);
-- Supports the Kanban board's "Filter: Technician" (UI Design Spec 3.3).
CREATE INDEX idx_repair_order_lines_technicians ON repair_order_lines USING gin (assigned_technicians);

ALTER TABLE repair_order_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE repair_order_lines FORCE ROW LEVEL SECURITY;

CREATE POLICY repair_order_lines_rls_policy ON repair_order_lines
FOR ALL
USING (garage_id = app_current_garage_id())
WITH CHECK (garage_id = app_current_garage_id());

COMMENT ON POLICY repair_order_lines_rls_policy ON repair_order_lines IS
'Restricts access to the transaction''s garage (app.current_garage_id, see app_current_garage_id()).';
