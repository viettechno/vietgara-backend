package repository

import (
	"context"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// IssueCost is the cost of the parts issued to one repair order.
type IssueCost struct {
	// HasIssue is true when the repair order has a stock issue at all.
	HasIssue bool
	// Posted is true once an issue of the order is posted: its cost is then
	// the snapshot taken when the stock left.
	Posted bool
	// ByLine is the cost per repair order line, whole VND, from the posted
	// issues.
	ByLine map[string]int64
}

// IssueCosts returns the cost of the posted stock issues of the repair orders
// (FR-RPT-05): quantity times the unit cost snapshot, less the unused parts
// returned to stock at their snapshot, summed per repair order line and
// rounded half up to whole VND. Cancelled issues do not count. An order with no entry has no issue.
func (r *PostgresRepository) IssueCosts(ctx context.Context, garageID string, repairOrderIDs []string) (map[string]IssueCost, error) {
	q := platformdb.QuerierFromContext(ctx, r.pool)
	out := map[string]IssueCost{}

	rows, err := q.Query(ctx, `
		SELECT repair_order_id::text, bool_or(status = 'posted')
		FROM stock_issues WHERE garage_id = $1 AND repair_order_id = ANY($2::uuid[]) AND status <> 'cancelled' GROUP BY repair_order_id`, garageID, repairOrderIDs)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var (
			id     string
			posted bool
		)

		if err := rows.Scan(&id, &posted); err != nil {
			rows.Close()

			return nil, err
		}

		out[id] = IssueCost{HasIssue: true, Posted: posted, ByLine: map[string]int64{}}
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, err
	}

	lines, err := q.Query(ctx, `
		SELECT si.repair_order_id::text, l.repair_order_line_id::text, round(sum(l.quantity * COALESCE(l.unit_cost, 0) - COALESCE((
			SELECT sum(r.quantity * COALESCE(r.unit_cost, 0)) FROM repair_order_part_returns r
			WHERE r.garage_id = l.garage_id AND r.stock_issue_line_id = l.id), 0)))::bigint
		FROM stock_issue_lines l JOIN stock_issues si ON si.garage_id = l.garage_id AND si.id = l.stock_issue_id
		WHERE l.garage_id = $1 AND si.repair_order_id = ANY($2::uuid[]) AND si.status = 'posted'
		GROUP BY si.repair_order_id, l.repair_order_line_id`, garageID, repairOrderIDs)
	if err != nil {
		return nil, err
	}
	defer lines.Close()

	for lines.Next() {
		var (
			orderID, lineID string
			cost            int64
		)

		if err := lines.Scan(&orderID, &lineID, &cost); err != nil {
			return nil, err
		}

		// An issue posted between the two statements has no entry yet.
		c, ok := out[orderID]
		if !ok {
			c = IssueCost{HasIssue: true, Posted: true, ByLine: map[string]int64{}}
		}

		c.ByLine[lineID] = cost
		out[orderID] = c
	}

	return out, lines.Err()
}
