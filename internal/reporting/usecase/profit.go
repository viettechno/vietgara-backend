package usecase

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ProfitGroupBy chooses what a profit report adds up by (FR-RPT-05).
type ProfitGroupBy string

// Valid ProfitGroupBy values.
const (
	GroupByRepairOrder ProfitGroupBy = "repair_order"
	GroupByJob         ProfitGroupBy = "job"
	GroupByTechnician  ProfitGroupBy = "technician"
)

// ProfitRow is the profit of one repair order, job or technician.
type ProfitRow struct {
	// Key is the repair order, job or technician id; empty for the lines that
	// belong to no job or no technician.
	Key          string
	Label        string
	RepairOrders int
	Revenue      int64
	Cost         int64
	Profit       int64
}

// ProfitReport is the profit of the repair orders closed in a period.
type ProfitReport struct {
	Rows         []ProfitRow
	TotalRevenue int64
	TotalCost    int64
	TotalProfit  int64
	// OrdersWithoutCost counts the orders of the report whose stock issue is
	// not posted yet: their cost counts as zero.
	OrdersWithoutCost int
}

// GetProfitReportRequest is the input of GetProfitReport.
type GetProfitReportRequest struct {
	GarageID string
	GroupBy  ProfitGroupBy
	// From is inclusive, To exclusive; calendar dates of the garage's zone.
	From, To time.Time
}

// RepairOrderProfit is the profit of one repair order.
type RepairOrderProfit struct {
	Revenue int64
	Cost    int64
	Profit  int64
	// CostKnown is false until the stock issue is posted; the profit is then
	// not shown (it is 0).
	CostKnown bool
}

// GetProfitReport adds up the revenue of the billable lines of the repair
// orders closed in the period and the cost snapshots of the parts issued for
// them, by repair order, job or technician (FR-RPT-05). A line shared by n
// technicians is split equally between them, the remainder going to the first
// by id, so the rows add up to the totals whichever way they are grouped.
func (uc *ReportingUseCase) GetProfitReport(ctx context.Context, req GetProfitReportRequest) (*ProfitReport, error) {
	const op = "get profit report"

	if !slices.Contains([]ProfitGroupBy{GroupByRepairOrder, GroupByJob, GroupByTechnician}, req.GroupBy) {
		return nil, apperror.Validation("groupBy", apperror.IssueRequired, "groupBy must be REPAIR_ORDER, JOB or TECHNICIAN")
	}

	from, to := calendarDate(req.From), calendarDate(req.To)
	if err := checkRange(from, to); err != nil {
		return nil, err
	}

	timezone, err := uc.Garages.Timezone(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	lines, err := uc.Orders.ClosedLines(ctx, req.GarageID, timezone, from, to)
	if err != nil {
		return nil, toAppError(op, err)
	}

	costs, err := uc.Costs.IssueCosts(ctx, req.GarageID, orderIDs(lines))
	if err != nil {
		return nil, toAppError(op, err)
	}

	labels, err := uc.labels(ctx, req.GarageID, req.GroupBy, lines)
	if err != nil {
		return nil, toAppError(op, err)
	}

	return buildProfit(req.GroupBy, lines, costs, labels), nil
}

// GetRepairOrderProfit is the profit of one repair order (FR-RPT-05): known
// once its stock issue is posted.
func (uc *ReportingUseCase) GetRepairOrderProfit(ctx context.Context, garageID, repairOrderID string) (*RepairOrderProfit, error) {
	const op = "get repair order profit"

	if uuid.Validate(repairOrderID) != nil {
		return nil, toAppError(op, ErrOrderNotFound)
	}

	lines, err := uc.Orders.OrderLines(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	costs, err := uc.Costs.IssueCosts(ctx, garageID, []string{repairOrderID})
	if err != nil {
		return nil, toAppError(op, err)
	}

	cost := costs[repairOrderID]
	out := &RepairOrderProfit{CostKnown: costKnown(cost, lines)}

	for i := range lines {
		out.Revenue += lines[i].Revenue
		out.Cost += cost.ByLine[lines[i].LineID]
	}

	if out.CostKnown {
		out.Profit = out.Revenue - out.Cost
	}

	return out, nil
}

// labels names the rows of a report grouped by job or technician.
func (uc *ReportingUseCase) labels(ctx context.Context, garageID string, groupBy ProfitGroupBy, lines []ProfitLine) (map[string]string, error) {
	switch groupBy {
	case GroupByJob:
		var raw []string
		for i := range lines {
			raw = append(raw, lines[i].JobID)
		}

		ids := distinct(raw)
		if len(ids) == 0 {
			return map[string]string{}, nil
		}

		return uc.Names.JobNames(ctx, garageID, ids)
	case GroupByTechnician:
		var raw []string
		for i := range lines {
			raw = append(raw, lines[i].TechnicianIDs...)
		}

		ids := distinct(raw)
		if len(ids) == 0 {
			return map[string]string{}, nil
		}

		return uc.Names.UserNames(ctx, ids)
	case GroupByRepairOrder:
		return map[string]string{}, nil
	}

	return map[string]string{}, nil
}

func orderIDs(lines []ProfitLine) []string {
	var (
		ids  []string
		seen = map[string]bool{}
	)

	for i := range lines {
		if !seen[lines[i].RepairOrderID] {
			seen[lines[i].RepairOrderID] = true
			ids = append(ids, lines[i].RepairOrderID)
		}
	}

	return ids
}

// distinct returns ids without repeats or blanks, in order of appearance.
func distinct(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}

	return out
}

// costKnown tells whether the cost of an order is known: its stock issue is
// posted, or it has nothing to issue (no issue and no part line).
func costKnown(cost IssueCost, lines []ProfitLine) bool {
	if cost.Posted {
		return true
	}

	if cost.HasIssue {
		return false
	}

	for i := range lines {
		if lines[i].Consumes {
			return false
		}
	}

	return true
}

// share is one row's part of a line.
type share struct {
	key, label    string
	order         string
	revenue, cost int64
}

// split divides total between n parts equally, the remainder going to the first.
func split(total int64, n, i int) int64 {
	each := total / int64(n)
	if i == 0 {
		return each + total - each*int64(n)
	}

	return each
}

// buildProfit adds the lines up by the grouping.
func buildProfit(groupBy ProfitGroupBy, lines []ProfitLine, costs map[string]IssueCost, labels map[string]string) *ProfitReport {
	byOrder := map[string][]ProfitLine{}

	for i := range lines {
		byOrder[lines[i].RepairOrderID] = append(byOrder[lines[i].RepairOrderID], lines[i])
	}

	var shares []share

	report := &ProfitReport{}

	for _, id := range orderIDs(lines) {
		if !costKnown(costs[id], byOrder[id]) {
			report.OrdersWithoutCost++
		}

		for _, l := range byOrder[id] {
			shares = append(shares, sharesOf(groupBy, l, costs[id].ByLine[l.LineID], labels)...)
		}
	}

	rows := map[string]*ProfitRow{}
	orders := map[string]map[string]bool{}

	for _, s := range shares {
		row, ok := rows[s.key]
		if !ok {
			row = &ProfitRow{Key: s.key, Label: s.label}
			rows[s.key], orders[s.key] = row, map[string]bool{}
		}

		row.Revenue += s.revenue
		row.Cost += s.cost
		orders[s.key][s.order] = true
	}

	for key, row := range rows {
		row.RepairOrders, row.Profit = len(orders[key]), row.Revenue-row.Cost
		report.TotalRevenue += row.Revenue
		report.TotalCost += row.Cost
		report.Rows = append(report.Rows, *row)
	}

	report.TotalProfit = report.TotalRevenue - report.TotalCost

	sort.Slice(report.Rows, func(i, j int) bool {
		if report.Rows[i].Profit != report.Rows[j].Profit {
			return report.Rows[i].Profit > report.Rows[j].Profit
		}

		return report.Rows[i].Key < report.Rows[j].Key
	})

	return report
}

// sharesOf turns one line into the shares of the rows it belongs to; a line
// that earned and cost nothing makes none.
func sharesOf(groupBy ProfitGroupBy, l ProfitLine, cost int64, labels map[string]string) []share {
	// Grouped by repair order every closed order has a row; the other groupings
	// skip a line that earned and cost nothing.
	if l.Revenue == 0 && cost == 0 && groupBy != GroupByRepairOrder {
		return nil
	}

	switch groupBy {
	case GroupByJob:
		return []share{{key: l.JobID, label: labels[l.JobID], order: l.RepairOrderID, revenue: l.Revenue, cost: cost}}
	case GroupByTechnician:
		if len(l.TechnicianIDs) == 0 {
			return []share{{order: l.RepairOrderID, revenue: l.Revenue, cost: cost}}
		}

		ids := slices.Clone(l.TechnicianIDs)
		slices.Sort(ids)

		ids = slices.Compact(ids)
		out := make([]share, len(ids))

		for i, id := range ids {
			out[i] = share{
				key: id, label: labels[id], order: l.RepairOrderID,
				revenue: split(l.Revenue, len(ids), i), cost: split(cost, len(ids), i),
			}
		}

		return out
	case GroupByRepairOrder:
	}

	return []share{{key: l.RepairOrderID, label: l.RepairOrderNumber, order: l.RepairOrderID, revenue: l.Revenue, cost: cost}}
}
