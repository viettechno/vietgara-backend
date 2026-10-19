-- Release 7.3: the job catalogue (FR-CAT-05, FR-CAT-08), the quote and
-- repair order line columns that remember a job and mark hidden
-- consumables, repair order cancellation, and the waiting-on-parts marks
-- of repair orders and stock issues (FR-INV-15). jobs and job_parts are
-- owned by Catalogue; the other columns by the module of their table.

-- ---------------------------------------------------------------------------
-- jobs: a type of work with a standard labor charge (FR-CAT-05).
-- ---------------------------------------------------------------------------
CREATE TABLE jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL REFERENCES garages(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    -- A free label to group jobs in the picker.
    category VARCHAR(100) NOT NULL DEFAULT '',
    -- Whole VND; zero is allowed.
    labor_price BIGINT NOT NULL DEFAULT 0 CHECK (labor_price >= 0),
    duration_minutes INTEGER NOT NULL DEFAULT 0 CHECK (duration_minutes >= 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT jobs_garage_id_id_key UNIQUE (garage_id, id)
);

CREATE UNIQUE INDEX idx_jobs_garage_name_unique ON jobs (garage_id, lower(name)) WHERE deleted_at IS NULL;
CREATE INDEX idx_jobs_garage_category ON jobs (garage_id, lower(category)) WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- job_parts: the parts a job suggests. quantity is the amount typed in
-- unit_id (the base unit when NULL). A suggested part is listed once per
-- job. The unit key is composite so the unit is a packaging unit of the
-- same part; removing such a packaging unit is refused while a job uses it.
-- ---------------------------------------------------------------------------
CREATE TABLE job_parts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    garage_id UUID NOT NULL,
    job_id UUID NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    part_id UUID NOT NULL,
    unit_id UUID,
    quantity NUMERIC(14, 3) NOT NULL CHECK (quantity > 0),
    -- A hidden consumable (FR-CAT-08).
    non_billable BOOLEAN NOT NULL DEFAULT FALSE,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT job_parts_garage_job_fkey FOREIGN KEY (garage_id, job_id)
        REFERENCES jobs (garage_id, id) ON DELETE CASCADE,
    CONSTRAINT job_parts_garage_part_fkey FOREIGN KEY (garage_id, part_id)
        REFERENCES parts (garage_id, id) ON DELETE RESTRICT,
    CONSTRAINT job_parts_garage_part_unit_fkey FOREIGN KEY (garage_id, part_id, unit_id)
        REFERENCES part_units (garage_id, part_id, unit_id) ON DELETE RESTRICT,
    CONSTRAINT job_parts_job_part_key UNIQUE (job_id, part_id)
);

CREATE INDEX idx_job_parts_part ON job_parts (garage_id, part_id);

DO $$
DECLARE
    t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['jobs', 'job_parts']
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

-- ---------------------------------------------------------------------------
-- Lines remember their job and can be hidden consumables. job_id is not a
-- foreign key: a job that a line came from is deactivated, never deleted
-- (FR-CAT-07), and the lines live in other modules.
-- ---------------------------------------------------------------------------
ALTER TABLE quote_lines
    ADD COLUMN job_id UUID,
    ADD COLUMN non_billable BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE repair_order_lines
    ADD COLUMN job_id UUID,
    ADD COLUMN non_billable BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX idx_quote_lines_job ON quote_lines (garage_id, job_id) WHERE job_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Repair orders: a cancelled status, and the waiting-on-parts mark set from
-- Inventory's answer (the draft stock issue cannot be covered). The mark is
-- stored so a later edit of a line does not lose it.
-- ---------------------------------------------------------------------------
ALTER TYPE repair_order_status ADD VALUE IF NOT EXISTS 'cancelled';

ALTER TABLE repair_orders ADD COLUMN waiting_on_parts BOOLEAN NOT NULL DEFAULT FALSE;

-- The stock issue's own answer to "can the stock cover this draft?", kept to
-- tell the repair order only when it changes.
ALTER TABLE stock_issues ADD COLUMN is_short BOOLEAN NOT NULL DEFAULT FALSE;

-- Reservations are served in the order the issues were created. NOW() is the
-- start of the transaction, so two issues made by one transaction would tie;
-- the clock at the statement keeps their order.
ALTER TABLE stock_issues ALTER COLUMN created_at SET DEFAULT clock_timestamp();
