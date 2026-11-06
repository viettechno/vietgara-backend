package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

// TechnicianWork is what one technician completed in a period (FR-RPT-02).
type TechnicianWork struct {
	TechnicianID   string
	CompletedLines int
	RepairOrders   int
	// AverageTurnaroundMinutes is the mean of completed_at - started_at over
	// the lines that have both, in whole minutes; 0 when none has.
	AverageTurnaroundMinutes int64
}

// CompletedWork returns, per technician assigned to them, the billable lines
// completed in [from, to) (calendar dates of timezone) of repair orders that
// are not cancelled. A line shared by two technicians counts for both.
func (r *PostgresRepository) CompletedWork(ctx context.Context, garageID, timezone string, from, to time.Time) ([]TechnicianWork, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT x.tech::text, count(*)::int, count(DISTINCT x.repair_order_id)::int,
			COALESCE(round(avg(extract(epoch FROM x.completed_at - x.started_at) / 60) FILTER (WHERE x.started_at IS NOT NULL))::bigint, 0)
		FROM (
			SELECT DISTINCT l.id, l.repair_order_id, l.started_at, l.completed_at, t.tech
			FROM repair_order_lines l
			JOIN repair_orders ro ON ro.garage_id = l.garage_id AND ro.id = l.repair_order_id
			CROSS JOIN LATERAL unnest(l.assigned_technicians) AS t(tech)
			WHERE l.garage_id = $1 AND ro.deleted_at IS NULL AND ro.status <> 'cancelled' AND NOT l.non_billable
				AND l.completed_at >= ($2::date::timestamp AT TIME ZONE $4) AND l.completed_at < ($3::date::timestamp AT TIME ZONE $4)
		) x
		GROUP BY x.tech`,
		garageID, from.Format(time.DateOnly), to.Format(time.DateOnly), timezone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TechnicianWork

	for rows.Next() {
		var w TechnicianWork
		if err := rows.Scan(&w.TechnicianID, &w.CompletedLines, &w.RepairOrders, &w.AverageTurnaroundMinutes); err != nil {
			return nil, err
		}

		out = append(out, w)
	}

	return out, rows.Err()
}

// ProfitLine is one line of a repair order as the profit report sees it.
type ProfitLine struct {
	RepairOrderID     string
	RepairOrderNumber string
	LineID            string
	// JobID is empty for a line of no job.
	JobID string
	// Revenue is the line's total; a hidden consumable (non-billable) has none.
	Revenue int64
	// Consumes is true for a part line, which the stock issue covers.
	Consumes      bool
	TechnicianIDs []string
}

const profitLineColumns = `ro.id::text, ro.number, l.id::text, COALESCE(l.job_id::text, ''),
	CASE WHEN l.non_billable THEN 0 ELSE l.line_total END, l.item_type::text = 'part', l.assigned_technicians::text[]`

func scanProfitLines(rows pgx.Rows) ([]ProfitLine, error) {
	defer rows.Close()

	var out []ProfitLine

	for rows.Next() {
		var l ProfitLine
		if err := rows.Scan(&l.RepairOrderID, &l.RepairOrderNumber, &l.LineID, &l.JobID, &l.Revenue, &l.Consumes, &l.TechnicianIDs); err != nil {
			return nil, err
		}

		out = append(out, l)
	}

	return out, rows.Err()
}

// ClosedLines returns every line of the repair orders closed in [from, to)
// (calendar dates of timezone), order by order.
func (r *PostgresRepository) ClosedLines(ctx context.Context, garageID, timezone string, from, to time.Time) ([]ProfitLine, error) {
	rows, err := platformdb.QuerierFromContext(ctx, r.pool).Query(ctx, `
		SELECT `+profitLineColumns+`
		FROM repair_orders ro JOIN repair_order_lines l ON l.garage_id = ro.garage_id AND l.repair_order_id = ro.id
		WHERE ro.garage_id = $1 AND ro.deleted_at IS NULL AND ro.status = 'closed'
			AND ro.closed_at >= ($2::date::timestamp AT TIME ZONE $4) AND ro.closed_at < ($3::date::timestamp AT TIME ZONE $4)
		ORDER BY ro.closed_at, ro.id, l.position`,
		garageID, from.Format(time.DateOnly), to.Format(time.DateOnly), timezone)
	if err != nil {
		return nil, err
	}

	return scanProfitLines(rows)
}

// OrderLines returns every line of one repair order (a cancelled order is not
// found), or domain.ErrRepairOrderNotFound.
func (r *PostgresRepository) OrderLines(ctx context.Context, garageID, repairOrderID string) ([]ProfitLine, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	var exists bool

	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repair_orders WHERE garage_id = $1 AND id = $2 AND deleted_at IS NULL AND status <> 'cancelled')`,
		garageID, repairOrderID).Scan(&exists)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, domain.ErrRepairOrderNotFound
	}

	rows, err := q.Query(ctx, `
		SELECT `+profitLineColumns+`
		FROM repair_orders ro JOIN repair_order_lines l ON l.garage_id = ro.garage_id AND l.repair_order_id = ro.id
		WHERE ro.garage_id = $1 AND ro.id = $2
		ORDER BY l.position`, garageID, repairOrderID)
	if err != nil {
		return nil, err
	}

	return scanProfitLines(rows)
}
