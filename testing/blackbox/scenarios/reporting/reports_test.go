// Scenario: E2E-RPT-001 to E2E-RPT-006
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/17_Reports.md
package reporting_test

import (
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// reportKit is a garage with two technicians, oil (80,000 per litre on
// average), grease, a filter that is out of stock and the job "Oil service".
type reportKit struct {
	*fixture.Business

	techA, techB *fixture.Actor
	oil, grease  *cataloguev1.Part
	filter       *cataloguev1.Part
	job          *cataloguev1.Job
	from, to     string
}

func newReportKit(t *testing.T, label string) *reportKit {
	t.Helper()

	ctx := t.Context()
	b := fixture.NewBusiness(t, label)
	k := &reportKit{Business: b}
	k.techA, k.techB = b.AddStaff(t, "Technician"), b.AddStaff(t, "Technician")

	unit := fixture.NewUnit(t, b.Owner.API, b.GarageID, "Litre", "L")
	suffix := strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)
	k.oil = fixture.NewPart(t, b.Owner.API, b.GarageID, "OIL-"+suffix, "Oil 5W-30", unit)
	k.grease = fixture.NewPart(t, b.Owner.API, b.GarageID, "GRS-"+suffix, "Grease", unit)
	k.filter = fixture.NewPart(t, b.Owner.API, b.GarageID, "FLT-"+suffix, "Filter", unit)

	_, err := b.Owner.API.ReceiveStock(ctx, b.GarageID, k.oil.GetId(), "100", "", 80_000)
	require.NoError(t, err)

	_, err = b.Owner.API.ReceiveStock(ctx, b.GarageID, k.grease.GetId(), "10", "", 5_000)
	require.NoError(t, err)

	k.job, err = b.Owner.API.CreateJob(ctx, b.GarageID, &cataloguev1.Job{
		Name: "Oil service " + suffix, Category: "Maintenance", LaborPrice: 300_000, DurationMinutes: 45,
		Parts: []*cataloguev1.JobPart{{PartId: k.oil.GetId(), Quantity: "2"}},
	})
	require.NoError(t, err)

	// A window around today, wide enough for the garage's time zone.
	k.from, k.to = time.Now().AddDate(0, 0, -2).Format(time.DateOnly), time.Now().AddDate(0, 0, 2).Format(time.DateOnly)

	return k
}

func (k *reportKit) id(a *fixture.Actor) string { return a.Session.GetUser().GetId() }

func labor(description string, price int64) *repairorderv1.RepairOrderLine {
	return &repairorderv1.RepairOrderLine{
		ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: description, Quantity: "1", UnitPrice: price,
	}
}

func part(p *cataloguev1.Part, quantity string, price int64) *repairorderv1.RepairOrderLine {
	return &repairorderv1.RepairOrderLine{
		ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART, ItemRefId: new(p.GetId()),
		Description: p.GetName(), Quantity: quantity, UnitPrice: price,
	}
}

func (k *reportKit) create(t *testing.T, lines ...*repairorderv1.RepairOrderLine) *repairorderv1.RepairOrder {
	t.Helper()

	order, err := k.Owner.API.CreateRepairOrder(t.Context(), k.GarageID, &repairorderv1.RepairOrder{VehicleId: k.VehicleID, Lines: lines})
	require.NoError(t, err)

	return order
}

// assign puts technicians on a line.
func (k *reportKit) assign(t *testing.T, order *repairorderv1.RepairOrder, line *repairorderv1.RepairOrderLine, techs ...*fixture.Actor) {
	t.Helper()

	ids := make([]string, len(techs))
	for i, tech := range techs {
		ids[i] = k.id(tech)
	}

	_, err := k.Owner.API.UpdateRepairOrderLineMasked(t.Context(), k.GarageID, order.GetId(), line.GetId(),
		&repairorderv1.RepairOrderLine{AssignedTechnicianIds: ids}, "assigned_technician_ids")
	require.NoError(t, err)
}

func (k *reportKit) complete(t *testing.T, order *repairorderv1.RepairOrder, line *repairorderv1.RepairOrderLine) {
	t.Helper()

	_, err := k.Owner.API.UpdateRepairOrderLine(t.Context(), k.GarageID, order.GetId(), line.GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	require.NoError(t, err)
}

// finish completes every billable line and closes the order.
func (k *reportKit) finish(t *testing.T, order *repairorderv1.RepairOrder) {
	t.Helper()

	current, err := k.Owner.API.GetRepairOrder(t.Context(), k.GarageID, order.GetId())
	require.NoError(t, err)

	for _, l := range current.GetLines() {
		if !l.GetNonBillable() && l.GetStatus() != repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED {
			k.complete(t, current, l)
		}
	}

	_, err = k.Owner.API.CloseRepairOrder(t.Context(), k.GarageID, order.GetId())
	require.NoError(t, err)
}

// post waits for the draft stock issue of the order and posts it, which takes
// the cost snapshot.
func (k *reportKit) post(t *testing.T, order *repairorderv1.RepairOrder) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for {
		issue, err := k.Owner.API.GetRepairOrderStockIssue(t.Context(), k.GarageID, order.GetId())
		if err == nil && issue.GetStatus() == inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_POSTED {
			return
		}

		if err == nil {
			posted, postErr := k.Owner.API.PostStockIssue(t.Context(), k.GarageID, issue.GetId())
			require.NoError(t, postErr)
			require.Equal(t, inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_POSTED, posted.GetStatus())

			return
		}

		require.True(t, time.Now().Before(deadline), "the stock issue of %s never appeared: %v", order.GetNumber(), err)
		time.Sleep(250 * time.Millisecond)
	}
}

func rowsByKey(rows []*reportingv1.ProfitRow) map[string]*reportingv1.ProfitRow {
	out := map[string]*reportingv1.ProfitRow{}
	for _, r := range rows {
		out[r.GetKey()] = r
	}

	return out
}

// Scenario: E2E-RPT-001: the technician report counts completed lines (a shared
// line counts for both technicians), the orders they belong to, and leaves out
// unfinished lines.
func TestTechnicianReport(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newReportKit(t, "rpt-tech")
	a, b := k.techA, k.techB

	// R1: two lines done by A and one shared by A and B.
	r1 := k.create(t, labor("R1 one", 100_000), labor("R1 two", 100_000), labor("R1 shared", 100_000))
	k.assign(t, r1, r1.GetLines()[0], a)
	k.assign(t, r1, r1.GetLines()[1], a)
	k.assign(t, r1, r1.GetLines()[2], a, b)

	for _, l := range r1.GetLines() {
		k.complete(t, r1, l)
	}

	// R2: one line of A and one of B.
	r2 := k.create(t, labor("R2 A", 100_000), labor("R2 B", 100_000))
	k.assign(t, r2, r2.GetLines()[0], a)
	k.assign(t, r2, r2.GetLines()[1], b)
	k.complete(t, r2, r2.GetLines()[0])
	k.complete(t, r2, r2.GetLines()[1])

	// R3: a line of B that is not completed.
	r3 := k.create(t, labor("R3 B", 100_000))
	k.assign(t, r3, r3.GetLines()[0], b)

	report, err := k.Owner.API.GetTechnicianReport(ctx, k.GarageID, k.from, k.to)
	require.NoError(t, err)
	require.Len(t, report.GetTechnicians(), 2)

	first, second := report.GetTechnicians()[0], report.GetTechnicians()[1]
	assert.Equal(t, k.id(a), first.GetTechnicianId(), "the technician with the most completed lines comes first")
	assert.Equal(t, a.Session.GetUser().GetFullName(), first.GetTechnicianName())
	assert.EqualValues(t, 4, first.GetCompletedLines(), "the shared line counts for both")
	assert.EqualValues(t, 2, first.GetRepairOrders())
	assert.EqualValues(t, 2, second.GetCompletedLines())
	assert.EqualValues(t, 2, second.GetRepairOrders())
	assert.Zero(t, first.GetAverageTurnaroundMinutes(), "completed in the same moment it started")

	// Other periods hold none of it.
	past, err := k.Owner.API.GetTechnicianReport(ctx, k.GarageID, "2020-01-01", "2020-02-01")
	require.NoError(t, err)
	assert.Empty(t, past.GetTechnicians())
}

// Scenario: E2E-RPT-002: profit per repair order is revenue less the cost
// snapshot of the stock issue; an order whose issue is not posted counts with
// zero cost and is reported; a later change of the average cost leaves the
// first order alone.
func TestProfitPerRepairOrder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newReportKit(t, "rpt-profit")

	posted := k.create(t, labor("Oil change", 300_000), part(k.oil, "2", 120_000))
	k.finish(t, posted)
	k.post(t, posted)

	// The filter is out of stock: the issue cannot be posted.
	waiting := k.create(t, labor("Filter change", 300_000), part(k.filter, "2", 120_000))
	k.finish(t, waiting)

	report, err := k.Owner.API.GetProfitReport(ctx, k.GarageID, "REPAIR_ORDER", k.from, k.to)
	require.NoError(t, err)

	rows := rowsByKey(report.GetRows())
	require.Len(t, rows, 2)
	assert.Equal(t, posted.GetNumber(), rows[posted.GetId()].GetLabel())
	assert.Equal(t, int64(540_000), rows[posted.GetId()].GetRevenue())
	assert.Equal(t, int64(160_000), rows[posted.GetId()].GetCost())
	assert.Equal(t, int64(380_000), rows[posted.GetId()].GetProfit())
	assert.Equal(t, int64(540_000), rows[waiting.GetId()].GetRevenue())
	assert.Zero(t, rows[waiting.GetId()].GetCost())
	assert.EqualValues(t, 1, report.GetOrdersWithoutCost())
	assert.Equal(t, int64(1_080_000), report.GetTotalRevenue())
	assert.Equal(t, int64(160_000), report.GetTotalCost())
	assert.Equal(t, int64(920_000), report.GetTotalProfit())
	assert.Equal(t, int64(540_000), report.GetRows()[0].GetProfit(), "most profitable first")

	// A later receipt changes the average cost; the first order keeps its snapshot.
	_, err = k.Owner.API.ReceiveStock(ctx, k.GarageID, k.oil.GetId(), "100", "", 200_000)
	require.NoError(t, err)

	again, err := k.Owner.API.GetProfitReport(ctx, k.GarageID, "REPAIR_ORDER", k.from, k.to)
	require.NoError(t, err)
	assert.Equal(t, int64(160_000), rowsByKey(again.GetRows())[posted.GetId()].GetCost())
}

// Scenario: E2E-RPT-003: grouped by job, technician or repair order, the totals
// are the same; a hidden consumable follows its job, a shared line is split
// equally with the odd VND going to the first technician by id.
func TestProfitGroupingsAddUpToTheSameTotals(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newReportKit(t, "rpt-groups")
	jobID := k.job.GetId()

	jobLabor := labor("Oil service labor", 300_000)
	jobLabor.JobId = &jobID
	jobOil := part(k.oil, "2", 120_000)
	jobOil.JobId = &jobID
	consumable := part(k.grease, "1", 0)
	consumable.JobId, consumable.NonBillable = &jobID, true

	order := k.create(t, jobLabor, jobOil, consumable, labor("Shared", 100_001), labor("Nobody", 7_000))
	byDescription := map[string]*repairorderv1.RepairOrderLine{}

	for _, l := range order.GetLines() {
		byDescription[l.GetDescription()] = l
	}

	k.assign(t, order, byDescription[jobLabor.GetDescription()], k.techA)
	k.assign(t, order, byDescription[jobOil.GetDescription()], k.techA)
	k.assign(t, order, byDescription["Shared"], k.techA, k.techB)
	k.finish(t, order)
	k.post(t, order)

	totals := func(r *reportingv1.GetProfitReportResponse) [3]int64 {
		return [3]int64{r.GetTotalRevenue(), r.GetTotalCost(), r.GetTotalProfit()}
	}

	byJob, err := k.Owner.API.GetProfitReport(ctx, k.GarageID, "JOB", k.from, k.to)
	require.NoError(t, err)

	jobs := rowsByKey(byJob.GetRows())
	require.Len(t, jobs, 2)
	assert.Equal(t, k.job.GetName(), jobs[jobID].GetLabel())
	assert.Equal(t, int64(540_000), jobs[jobID].GetRevenue())
	assert.Equal(t, int64(165_000), jobs[jobID].GetCost(), "the oil and the hidden grease")
	assert.Equal(t, int64(107_001), jobs[""].GetRevenue(), "the lines of no job are one row")
	assert.Zero(t, jobs[""].GetCost())

	byTech, err := k.Owner.API.GetProfitReport(ctx, k.GarageID, "TECHNICIAN", k.from, k.to)
	require.NoError(t, err)

	ids := []string{k.id(k.techA), k.id(k.techB)}
	slices.Sort(ids)

	techs := rowsByKey(byTech.GetRows())
	require.Len(t, techs, 3)

	// The shared 100,001 goes 50,001 to the first technician by id and 50,000 to the other.
	share := map[string]int64{ids[0]: 50_001, ids[1]: 50_000}
	assert.Equal(t, share[k.id(k.techB)], techs[k.id(k.techB)].GetRevenue())
	assert.Equal(t, int64(540_000)+share[k.id(k.techA)], techs[k.id(k.techA)].GetRevenue())
	assert.Equal(t, int64(160_000), techs[k.id(k.techA)].GetCost())
	assert.Equal(t, int64(7_000), techs[""].GetRevenue(), "the line with no technician is one row")
	assert.Equal(t, int64(5_000), techs[""].GetCost(), "with the hidden grease, which has no technician")

	byOrder, err := k.Owner.API.GetProfitReport(ctx, k.GarageID, "REPAIR_ORDER", k.from, k.to)
	require.NoError(t, err)

	assert.Equal(t, [3]int64{647_001, 165_000, 482_001}, totals(byOrder))
	assert.Equal(t, totals(byOrder), totals(byJob))
	assert.Equal(t, totals(byOrder), totals(byTech))
}

// Scenario: E2E-RPT-004: the technician report needs report.read; the profit
// report also needs inventory.cost.read; another garage's owner is refused.
func TestReportPermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newReportKit(t, "rpt-perms")

	clerkRole, err := k.Owner.API.CreateRole(ctx, &tenantv1.Role{
		Name: "Report reader " + strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10), Permissions: []tenantv1.Permission{tenantv1.Permission_PERMISSION_REPORT_READ},
	})
	require.NoError(t, err)

	group, err := k.Owner.API.CreateStaffGroup(ctx, k.GarageID, &tenantv1.StaffGroup{Name: "Readers " + clerkRole.GetName(), RoleId: clerkRole.GetId()})
	require.NoError(t, err)

	clerk := joinGroup(t, k, group.GetName())
	advisor := k.AddStaff(t, "Service Advisor")
	foreign := fixture.NewBusiness(t, "rpt-foreign")

	read := func(api *client.Client, garageID string) (techErr, profitErr error) {
		_, techErr = api.GetTechnicianReport(ctx, garageID, k.from, k.to)
		_, profitErr = api.GetProfitReport(ctx, garageID, "JOB", k.from, k.to)

		return techErr, profitErr
	}

	techErr, profitErr := read(k.Owner.API, k.GarageID)
	require.NoError(t, techErr)
	require.NoError(t, profitErr)

	techErr, profitErr = read(clerk.API, k.GarageID)
	require.NoError(t, techErr)
	fixture.RequireAPIError(t, profitErr, http.StatusForbidden, "")

	techErr, profitErr = read(advisor.API, k.GarageID)
	fixture.RequireAPIError(t, techErr, http.StatusForbidden, "")
	fixture.RequireAPIError(t, profitErr, http.StatusForbidden, "")

	techErr, profitErr = read(foreign.Owner.API, k.GarageID)
	fixture.RequireAPIError(t, techErr, http.StatusForbidden, "")
	fixture.RequireAPIError(t, profitErr, http.StatusForbidden, "")
}

// joinGroup adds a member to the named staff group of the kit's garage.
func joinGroup(t *testing.T, k *reportKit, name string) *fixture.Actor {
	t.Helper()

	ctx := t.Context()

	groups, err := k.Owner.API.ListStaffGroups(ctx, k.GarageID)
	require.NoError(t, err)

	var groupID string

	for _, g := range groups {
		if g.GetName() == name {
			groupID = g.GetId()
		}
	}

	require.NotEmpty(t, groupID)

	member := fixture.NewVerifiedOwner(t, "report-member")
	candidate, err := k.Owner.API.LookupStaffCandidate(ctx, k.GarageID, member.Email)
	require.NoError(t, err)

	_, err = k.Owner.API.CreateInvitation(ctx, k.GarageID, candidate.GetUserId(), []string{groupID})
	require.NoError(t, err)

	invitations, err := member.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, invitations)

	_, err = member.API.AcceptInvitation(ctx, invitations[0].GetId())
	require.NoError(t, err)

	return member
}

// Scenario: E2E-RPT-005: the profit on a repair order is known once its issue
// is posted; the advisor, who may read repair orders but not costs, is refused.
func TestRepairOrderProfit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newReportKit(t, "rpt-order")

	known := k.create(t, labor("Oil change", 300_000), part(k.oil, "2", 120_000))
	k.finish(t, known)
	k.post(t, known)

	unknown := k.create(t, labor("Filter change", 300_000), part(k.filter, "2", 120_000))
	k.finish(t, unknown)

	profit, err := k.Owner.API.GetRepairOrderProfit(ctx, k.GarageID, known.GetId())
	require.NoError(t, err)
	assert.True(t, profit.GetCostKnown())
	assert.Equal(t, int64(540_000), profit.GetRevenue())
	assert.Equal(t, int64(160_000), profit.GetCost())
	assert.Equal(t, int64(380_000), profit.GetProfit())

	profit, err = k.Owner.API.GetRepairOrderProfit(ctx, k.GarageID, unknown.GetId())
	require.NoError(t, err)
	assert.False(t, profit.GetCostKnown(), "the stock issue is not posted")
	assert.Equal(t, int64(540_000), profit.GetRevenue())
	assert.Zero(t, profit.GetProfit())

	advisor := k.AddStaff(t, "Service Advisor")

	_, err = advisor.API.GetRepairOrderProfit(ctx, k.GarageID, known.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = k.Owner.API.GetRepairOrderProfit(ctx, k.GarageID, "00000000-0000-0000-0000-000000000000")
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	_, err = k.Owner.API.GetRepairOrderProfit(ctx, k.GarageID, "not-an-id")
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")
}

// Scenario: E2E-RPT-006: bad periods and groupings are refused with a
// validation error naming the field.
func TestReportPeriodRules(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newReportKit(t, "rpt-period")
	api := k.Owner.API

	cases := map[string]struct{ from, to, groupBy string }{
		"from after to":    {"2026-11-10", "2026-11-01", "JOB"},
		"from equals to":   {"2026-11-01", "2026-11-01", "JOB"},
		"not a date":       {"soon", "2026-11-01", "JOB"},
		"to not a date":    {"2026-11-01", "2026/11/05", "JOB"},
		"more than a year": {"2025-01-01", "2026-11-01", "JOB"},
		"no grouping":      {"2026-11-01", "2026-11-05", ""},
		"unknown grouping": {"2026-11-01", "2026-11-05", "WEEK"},
	}

	for name, c := range cases {
		_, err := api.GetProfitReport(ctx, k.GarageID, c.groupBy, c.from, c.to)
		require.Error(t, err, name)

		if c.groupBy == "WEEK" {
			// A name that is not in the enum never reaches the service: the gateway refuses it.
			fixture.RequireAPIError(t, err, http.StatusBadRequest, "")

			continue
		}

		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

		if c.groupBy == "JOB" {
			_, err = api.GetTechnicianReport(ctx, k.GarageID, c.from, c.to)
			fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
		}
	}

	// 366 days is the longest.
	_, err := api.GetProfitReport(ctx, k.GarageID, "JOB", "2025-11-01", "2026-11-02")
	require.NoError(t, err)

	// to is exclusive: an order closed today is outside a range that ends today in the garage's zone.
	k2 := newReportKit(t, "rpt-edge")
	order := k2.create(t, labor("Today", 100_000))
	k2.finish(t, order)

	today := time.Now().In(time.FixedZone("ICT", 7*3600)).Format(time.DateOnly)
	tomorrow := time.Now().In(time.FixedZone("ICT", 7*3600)).AddDate(0, 0, 1).Format(time.DateOnly)

	with, err := k2.Owner.API.GetProfitReport(ctx, k2.GarageID, "REPAIR_ORDER", today, tomorrow)
	require.NoError(t, err)
	assert.Len(t, with.GetRows(), 1)

	without, err := k2.Owner.API.GetProfitReport(ctx, k2.GarageID, "REPAIR_ORDER", time.Now().AddDate(0, 0, -10).Format(time.DateOnly), today)
	require.NoError(t, err)
	assert.Empty(t, without.GetRows(), "to is exclusive")
}
