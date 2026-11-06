package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/reporting/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

type reportDeps struct {
	Garages *mocks.MockReportingGarageReader
	Work    *mocks.MockReportingWorkReader
	Orders  *mocks.MockReportingOrderReader
	Costs   *mocks.MockReportingCostReader
	Names   *mocks.MockReportingNameReader
}

func newReportUseCase(t *testing.T) (*usecase.ReportingUseCase, *reportDeps) {
	t.Helper()

	d := &reportDeps{
		Garages: mocks.NewMockReportingGarageReader(t), Work: mocks.NewMockReportingWorkReader(t),
		Orders: mocks.NewMockReportingOrderReader(t), Costs: mocks.NewMockReportingCostReader(t), Names: mocks.NewMockReportingNameReader(t),
	}
	d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return(hcm, nil).Maybe()

	return usecase.NewReportingUseCase(usecase.Deps{Garages: d.Garages, Work: d.Work, Orders: d.Orders, Costs: d.Costs, Names: d.Names}), d
}

const (
	techA  = "aaaaaaaa-0000-0000-0000-000000000001"
	techB  = "aaaaaaaa-0000-0000-0000-000000000002"
	order1 = "bbbbbbbb-0000-0000-0000-000000000001"
	order2 = "bbbbbbbb-0000-0000-0000-000000000002"
	job1   = "cccccccc-0000-0000-0000-000000000001"
)

func TestReportingUseCase_GetTechnicianReport(t *testing.T) {
	t.Parallel()

	req := usecase.GetTechnicianReportRequest{GarageID: garageID, From: date("2026-11-01"), To: date("2026-12-01")}

	t.Run("names the technicians and puts the busiest first", func(t *testing.T) {
		t.Parallel()

		uc, d := newReportUseCase(t)
		d.Work.EXPECT().CompletedWork(mock.Anything, garageID, hcm, onDate("2026-11-01"), onDate("2026-12-01")).Return([]usecase.TechnicianWork{
			{TechnicianID: techB, CompletedLines: 2, RepairOrders: 2, AverageTurnaroundMinutes: 30},
			{TechnicianID: techA, CompletedLines: 4, RepairOrders: 2, AverageTurnaroundMinutes: 90},
			{TechnicianID: "aaaaaaaa-0000-0000-0000-000000000003", CompletedLines: 2, RepairOrders: 1},
		}, nil).Once()
		d.Names.EXPECT().UserNames(mock.Anything, mock.Anything).Return(map[string]string{techA: "An", techB: "Binh", "aaaaaaaa-0000-0000-0000-000000000003": "Anh"}, nil).Once()

		got, err := uc.GetTechnicianReport(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, "An", got[0].TechnicianName)
		assert.Equal(t, 4, got[0].CompletedLines)
		assert.Equal(t, "Anh", got[1].TechnicianName, "ties go by name")
		assert.Equal(t, "Binh", got[2].TechnicianName)
	})

	t.Run("a period with no work asks for no names", func(t *testing.T) {
		t.Parallel()

		uc, d := newReportUseCase(t)
		d.Work.EXPECT().CompletedWork(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return(nil, nil).Once()

		got, err := uc.GetTechnicianReport(t.Context(), req)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("refuses a bad range", func(t *testing.T) {
		t.Parallel()

		uc, _ := newReportUseCase(t)

		for name, r := range map[string]usecase.GetTechnicianReportRequest{
			"from after to":  {GarageID: garageID, From: date("2026-12-01"), To: date("2026-11-01")},
			"from equals to": {GarageID: garageID, From: date("2026-11-01"), To: date("2026-11-01")},
			"too long":       {GarageID: garageID, From: date("2025-01-01"), To: date("2026-11-01")},
		} {
			_, err := uc.GetTechnicianReport(t.Context(), r)
			assertAppError(t, err, apperror.CodeInvalidArgument)

			_ = name
		}

		// 366 days is the longest.
		ok, d := newReportUseCase(t)
		d.Work.EXPECT().CompletedWork(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return(nil, nil).Once()

		_, err := ok.GetTechnicianReport(t.Context(), usecase.GetTechnicianReportRequest{GarageID: garageID, From: date("2025-11-01"), To: date("2026-11-02")})
		require.NoError(t, err)
	})

	t.Run("fails when a collaborator does", func(t *testing.T) {
		t.Parallel()

		uc, d := newReportUseCase(t)
		d.Work.EXPECT().CompletedWork(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return(nil, errBoomReporting).Once()

		_, err := uc.GetTechnicianReport(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newReportUseCase(t)
		d.Work.EXPECT().CompletedWork(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return([]usecase.TechnicianWork{{TechnicianID: techA}}, nil).Once()
		d.Names.EXPECT().UserNames(mock.Anything, mock.Anything).Return(nil, errBoomReporting).Once()

		_, err = uc.GetTechnicianReport(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		bad, bd := newReportUseCase(t)
		bd.Garages.ExpectedCalls = nil
		bd.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoomReporting).Once()

		_, err = bad.GetTechnicianReport(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

// profitLines are two closed orders. Order 1: 300,000 of labor of the oil
// service (technicians A and B, odd amounts) and 240,001 of oil with its
// hidden consumable (no revenue, cost only); order 2: a line of no job and no
// technician.
func profitLines() []usecase.ProfitLine {
	return []usecase.ProfitLine{
		{RepairOrderID: order1, RepairOrderNumber: "LSC-1", LineID: "l1", JobID: job1, Revenue: 300_001, TechnicianIDs: []string{techB, techA}},
		{RepairOrderID: order1, RepairOrderNumber: "LSC-1", LineID: "l2", JobID: job1, Revenue: 240_000, Consumes: true, TechnicianIDs: []string{techA}},
		{RepairOrderID: order1, RepairOrderNumber: "LSC-1", LineID: "l3", JobID: job1, Revenue: 0, Consumes: true},
		{RepairOrderID: order2, RepairOrderNumber: "LSC-2", LineID: "l4", Revenue: 100_000, Consumes: true},
		{RepairOrderID: order2, RepairOrderNumber: "LSC-2", LineID: "l5", Revenue: 0},
	}
}

func profitCosts() map[string]usecase.IssueCost {
	return map[string]usecase.IssueCost{
		// The first order's issue is posted; the second has a draft issue.
		order1: {HasIssue: true, Posted: true, ByLine: map[string]int64{"l2": 160_001, "l3": 5}},
		order2: {HasIssue: true},
	}
}

//nolint:funlen // one table of groupings
func TestReportingUseCase_GetProfitReport(t *testing.T) {
	t.Parallel()

	req := func(by usecase.ProfitGroupBy) usecase.GetProfitReportRequest {
		return usecase.GetProfitReportRequest{GarageID: garageID, GroupBy: by, From: date("2026-11-01"), To: date("2026-12-01")}
	}

	setup := func(t *testing.T) (*usecase.ReportingUseCase, *reportDeps) {
		t.Helper()

		uc, d := newReportUseCase(t)
		d.Orders.EXPECT().ClosedLines(mock.Anything, garageID, hcm, onDate("2026-11-01"), onDate("2026-12-01")).Return(profitLines(), nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, []string{order1, order2}).Return(profitCosts(), nil).Once()

		return uc, d
	}

	t.Run("by repair order", func(t *testing.T) {
		t.Parallel()

		uc, _ := setup(t)

		got, err := uc.GetProfitReport(t.Context(), req(usecase.GroupByRepairOrder))
		require.NoError(t, err)
		require.Len(t, got.Rows, 2)
		assert.Equal(t, usecase.ProfitRow{Key: order1, Label: "LSC-1", RepairOrders: 1, Revenue: 540_001, Cost: 160_006, Profit: 379_995}, got.Rows[0])
		assert.Equal(t, usecase.ProfitRow{Key: order2, Label: "LSC-2", RepairOrders: 1, Revenue: 100_000, Cost: 0, Profit: 100_000}, got.Rows[1])
		assert.Equal(t, int64(640_001), got.TotalRevenue)
		assert.Equal(t, int64(160_006), got.TotalCost)
		assert.Equal(t, int64(479_995), got.TotalProfit)
		assert.Equal(t, 1, got.OrdersWithoutCost, "the second order's issue is not posted")
	})

	t.Run("by job: the consumable follows its job, lines of no job are one row", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Names.EXPECT().JobNames(mock.Anything, garageID, []string{job1}).Return(map[string]string{job1: "Oil service"}, nil).Once()

		got, err := uc.GetProfitReport(t.Context(), req(usecase.GroupByJob))
		require.NoError(t, err)
		require.Len(t, got.Rows, 2)
		assert.Equal(t, usecase.ProfitRow{Key: job1, Label: "Oil service", RepairOrders: 1, Revenue: 540_001, Cost: 160_006, Profit: 379_995}, got.Rows[0])
		assert.Equal(t, usecase.ProfitRow{Key: "", RepairOrders: 1, Revenue: 100_000, Profit: 100_000}, got.Rows[1])
		assert.Equal(t, int64(479_995), got.TotalProfit)
	})

	t.Run("by technician: a shared line is split, the odd VND going to the first by id", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Names.EXPECT().UserNames(mock.Anything, mock.Anything).Return(map[string]string{techA: "An", techB: "Binh"}, nil).Once()

		got, err := uc.GetProfitReport(t.Context(), req(usecase.GroupByTechnician))
		require.NoError(t, err)

		byKey := map[string]usecase.ProfitRow{}
		for _, r := range got.Rows {
			byKey[r.Key] = r
		}

		// A: half of 300,001 (150,001 as first by id) + all of 240,000, cost 160,001.
		assert.Equal(t, usecase.ProfitRow{Key: techA, Label: "An", RepairOrders: 1, Revenue: 390_001, Cost: 160_001, Profit: 230_000}, byKey[techA])
		assert.Equal(t, usecase.ProfitRow{Key: techB, Label: "Binh", RepairOrders: 1, Revenue: 150_000, Profit: 150_000}, byKey[techB])
		// The consumable (cost 5) and order 2's line have no technician.
		assert.Equal(t, int64(100_000), byKey[""].Revenue)
		assert.Equal(t, int64(5), byKey[""].Cost)
		assert.Equal(t, 2, byKey[""].RepairOrders)

		assert.Equal(t, int64(640_001), got.TotalRevenue, "the rows add up to the same totals")
		assert.Equal(t, int64(160_006), got.TotalCost)
		assert.Equal(t, int64(479_995), got.TotalProfit)
	})

	t.Run("an order with nothing to issue has a known cost of zero", func(t *testing.T) {
		t.Parallel()

		uc, d := newReportUseCase(t)
		d.Orders.EXPECT().ClosedLines(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return([]usecase.ProfitLine{
			{RepairOrderID: order1, RepairOrderNumber: "LSC-1", LineID: "l1", Revenue: 100},
		}, nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, []string{order1}).Return(map[string]usecase.IssueCost{}, nil).Once()

		got, err := uc.GetProfitReport(t.Context(), req(usecase.GroupByRepairOrder))
		require.NoError(t, err)
		assert.Zero(t, got.OrdersWithoutCost)

		// With a part line and no issue at all, the cost is not known.
		uc, d = newReportUseCase(t)
		d.Orders.EXPECT().ClosedLines(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return([]usecase.ProfitLine{
			{RepairOrderID: order1, LineID: "l1", Revenue: 100, Consumes: true},
		}, nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, []string{order1}).Return(nil, nil).Once()

		got, err = uc.GetProfitReport(t.Context(), req(usecase.GroupByRepairOrder))
		require.NoError(t, err)
		assert.Equal(t, 1, got.OrdersWithoutCost)
	})

	t.Run("every closed order has a row by order, even one that earned and cost nothing", func(t *testing.T) {
		t.Parallel()

		zero := []usecase.ProfitLine{{RepairOrderID: order1, RepairOrderNumber: "LSC-1", LineID: "l1"}}

		uc, d := newReportUseCase(t)
		d.Orders.EXPECT().ClosedLines(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return(zero, nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, []string{order1}).Return(nil, nil).Once()

		got, err := uc.GetProfitReport(t.Context(), req(usecase.GroupByRepairOrder))
		require.NoError(t, err)
		require.Len(t, got.Rows, 1)
		assert.Equal(t, 1, got.Rows[0].RepairOrders)

		uc, d = newReportUseCase(t)
		d.Orders.EXPECT().ClosedLines(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return(zero, nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, []string{order1}).Return(nil, nil).Once()

		got, err = uc.GetProfitReport(t.Context(), req(usecase.GroupByJob))
		require.NoError(t, err)
		assert.Empty(t, got.Rows, "the other groupings skip a line with nothing in it")
	})

	t.Run("an empty period has no rows", func(t *testing.T) {
		t.Parallel()

		uc, d := newReportUseCase(t)
		d.Orders.EXPECT().ClosedLines(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return(nil, nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, []string(nil)).Return(nil, nil).Once()

		got, err := uc.GetProfitReport(t.Context(), req(usecase.GroupByJob))
		require.NoError(t, err)
		assert.Empty(t, got.Rows)
		assert.Zero(t, got.TotalProfit)
	})

	t.Run("refuses a missing grouping and a bad range", func(t *testing.T) {
		t.Parallel()

		uc, _ := newReportUseCase(t)

		_, err := uc.GetProfitReport(t.Context(), req(""))
		assertAppError(t, err, apperror.CodeInvalidArgument)

		bad := req(usecase.GroupByJob)
		bad.From = bad.To

		_, err = uc.GetProfitReport(t.Context(), bad)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("fails when a collaborator does", func(t *testing.T) {
		t.Parallel()

		uc, d := newReportUseCase(t)
		d.Orders.EXPECT().ClosedLines(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return(nil, errBoomReporting).Once()

		_, err := uc.GetProfitReport(t.Context(), req(usecase.GroupByJob))
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newReportUseCase(t)
		d.Orders.EXPECT().ClosedLines(mock.Anything, garageID, hcm, mock.Anything, mock.Anything).Return(profitLines(), nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, mock.Anything).Return(nil, errBoomReporting).Once()

		_, err = uc.GetProfitReport(t.Context(), req(usecase.GroupByJob))
		assertAppError(t, err, apperror.CodeInternal)

		for _, by := range []usecase.ProfitGroupBy{usecase.GroupByJob, usecase.GroupByTechnician} {
			uc, d = setup(t)
			d.Names.EXPECT().JobNames(mock.Anything, garageID, mock.Anything).Return(nil, errBoomReporting).Maybe()
			d.Names.EXPECT().UserNames(mock.Anything, mock.Anything).Return(nil, errBoomReporting).Maybe()

			_, err = uc.GetProfitReport(t.Context(), req(by))
			assertAppError(t, err, apperror.CodeInternal)
		}

		bad, bd := newReportUseCase(t)
		bd.Garages.ExpectedCalls = nil
		bd.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoomReporting).Once()

		_, err = bad.GetProfitReport(t.Context(), req(usecase.GroupByJob))
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestReportingUseCase_GetRepairOrderProfit(t *testing.T) {
	t.Parallel()

	t.Run("the profit of an order whose issue is posted", func(t *testing.T) {
		t.Parallel()

		uc, d := newReportUseCase(t)
		d.Orders.EXPECT().OrderLines(mock.Anything, garageID, order1).Return(profitLines()[:3], nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, []string{order1}).Return(profitCosts(), nil).Once()

		got, err := uc.GetRepairOrderProfit(t.Context(), garageID, order1)
		require.NoError(t, err)
		assert.Equal(t, usecase.RepairOrderProfit{Revenue: 540_001, Cost: 160_006, Profit: 379_995, CostKnown: true}, *got)
	})

	t.Run("before the issue is posted the profit is not known", func(t *testing.T) {
		t.Parallel()

		uc, d := newReportUseCase(t)
		d.Orders.EXPECT().OrderLines(mock.Anything, garageID, order2).Return(profitLines()[3:], nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, []string{order2}).Return(profitCosts(), nil).Once()

		got, err := uc.GetRepairOrderProfit(t.Context(), garageID, order2)
		require.NoError(t, err)
		assert.Equal(t, usecase.RepairOrderProfit{Revenue: 100_000}, *got)
	})

	t.Run("unknown orders and failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newReportUseCase(t)

		_, err := uc.GetRepairOrderProfit(t.Context(), garageID, "not-an-id")
		assertAppError(t, err, apperror.CodeNotFound)

		d.Orders.EXPECT().OrderLines(mock.Anything, garageID, order1).Return(nil, usecase.ErrOrderNotFound).Once()

		_, err = uc.GetRepairOrderProfit(t.Context(), garageID, order1)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Orders.EXPECT().OrderLines(mock.Anything, garageID, order1).Return(profitLines(), nil).Once()
		d.Costs.EXPECT().IssueCosts(mock.Anything, garageID, mock.Anything).Return(nil, errBoomReporting).Once()

		_, err = uc.GetRepairOrderProfit(t.Context(), garageID, order1)
		assertAppError(t, err, apperror.CodeInternal)
	})
}
