package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/reporting/handler"
	"github.com/viettechno/vietgara-backend/internal/reporting/usecase"
)

const (
	techID  = "aaaaaaaa-0000-0000-0000-000000000001"
	orderID = "bbbbbbbb-0000-0000-0000-000000000001"
)

type reportEnv struct {
	srv    *handler.Server
	work   *mocks.MockReportingWorkReader
	orders *mocks.MockReportingOrderReader
	costs  *mocks.MockReportingCostReader
	names  *mocks.MockReportingNameReader
}

func newReportEnv(t *testing.T) *reportEnv {
	t.Helper()

	garages := mocks.NewMockReportingGarageReader(t)
	garages.EXPECT().Timezone(anyArg, garageID).Return("UTC", nil).Maybe()

	e := &reportEnv{
		work: mocks.NewMockReportingWorkReader(t), orders: mocks.NewMockReportingOrderReader(t),
		costs: mocks.NewMockReportingCostReader(t), names: mocks.NewMockReportingNameReader(t),
	}
	e.srv = handler.NewServer(usecase.NewReportingUseCase(usecase.Deps{Garages: garages, Work: e.work, Orders: e.orders, Costs: e.costs, Names: e.names}))

	return e
}

func withCost(ctx context.Context) context.Context {
	return access.WithAccess(ctx, &access.Access{Permissions: []access.Permission{access.ReportRead, access.InventoryCostRead}})
}

func withoutCost(ctx context.Context) context.Context {
	return access.WithAccess(ctx, &access.Access{Permissions: []access.Permission{access.ReportRead}})
}

func TestGetTechnicianReport(t *testing.T) {
	t.Parallel()

	e := newReportEnv(t)
	e.work.EXPECT().CompletedWork(anyArg, garageID, "UTC", anyArg, anyArg).
		Return([]usecase.TechnicianWork{{TechnicianID: techID, CompletedLines: 3, RepairOrders: 2, AverageTurnaroundMinutes: 45}}, nil).Once()
	e.names.EXPECT().UserNames(anyArg, []string{techID}).Return(map[string]string{techID: "An"}, nil).Once()

	resp, err := e.srv.GetTechnicianReport(t.Context(), &reportingv1.GetTechnicianReportRequest{GarageId: garageID, From: "2026-11-01", To: "2026-12-01"})
	require.NoError(t, err)
	require.Len(t, resp.GetTechnicians(), 1)
	assert.Equal(t, "An", resp.GetTechnicians()[0].GetTechnicianName())
	assert.EqualValues(t, 3, resp.GetTechnicians()[0].GetCompletedLines())
	assert.EqualValues(t, 45, resp.GetTechnicians()[0].GetAverageTurnaroundMinutes())

	for _, bad := range []*reportingv1.GetTechnicianReportRequest{
		{GarageId: garageID, From: "soon", To: "2026-12-01"},
		{GarageId: garageID, From: "2026-11-01", To: "later"},
		{GarageId: garageID, From: "2026-12-01", To: "2026-11-01"},
	} {
		_, err = e.srv.GetTechnicianReport(t.Context(), bad)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	}
}

func TestGetProfitReport(t *testing.T) {
	t.Parallel()

	e := newReportEnv(t)
	e.orders.EXPECT().ClosedLines(anyArg, garageID, "UTC", anyArg, anyArg).Return([]usecase.ProfitLine{
		{RepairOrderID: orderID, RepairOrderNumber: "LSC-1", LineID: "l1", Revenue: 540_000, Consumes: true},
	}, nil).Once()
	e.costs.EXPECT().IssueCosts(anyArg, garageID, []string{orderID}).
		Return(map[string]usecase.IssueCost{orderID: {HasIssue: true, Posted: true, ByLine: map[string]int64{"l1": 160_000}}}, nil).Once()

	req := &reportingv1.GetProfitReportRequest{
		GarageId: garageID, GroupBy: reportingv1.ProfitGroupBy_PROFIT_GROUP_BY_REPAIR_ORDER, From: "2026-11-01", To: "2026-12-01",
	}

	resp, err := e.srv.GetProfitReport(withCost(t.Context()), req)
	require.NoError(t, err)
	require.Len(t, resp.GetRows(), 1)
	assert.Equal(t, "LSC-1", resp.GetRows()[0].GetLabel())
	assert.Equal(t, int64(380_000), resp.GetRows()[0].GetProfit())
	assert.Equal(t, int64(380_000), resp.GetTotalProfit())
	assert.Zero(t, resp.GetOrdersWithoutCost())

	_, err = e.srv.GetProfitReport(withoutCost(t.Context()), req)
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "every figure reveals the cost")

	_, err = e.srv.GetProfitReport(t.Context(), req)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	for _, bad := range []*reportingv1.GetProfitReportRequest{
		{GarageId: garageID, From: "2026-11-01", To: "2026-12-01"},
		{GarageId: garageID, GroupBy: reportingv1.ProfitGroupBy_PROFIT_GROUP_BY_JOB, From: "x", To: "2026-12-01"},
	} {
		_, err = e.srv.GetProfitReport(withCost(t.Context()), bad)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	}

	for _, by := range []reportingv1.ProfitGroupBy{reportingv1.ProfitGroupBy_PROFIT_GROUP_BY_JOB, reportingv1.ProfitGroupBy_PROFIT_GROUP_BY_TECHNICIAN} {
		e2 := newReportEnv(t)
		e2.orders.EXPECT().ClosedLines(anyArg, garageID, "UTC", anyArg, anyArg).Return(nil, nil).Once()
		e2.costs.EXPECT().IssueCosts(anyArg, garageID, mock.Anything).Return(nil, nil).Once()

		_, err = e2.srv.GetProfitReport(withCost(t.Context()), &reportingv1.GetProfitReportRequest{GarageId: garageID, GroupBy: by, From: "2026-11-01", To: "2026-12-01"})
		require.NoError(t, err)
	}
}

func TestGetRepairOrderProfit(t *testing.T) {
	t.Parallel()

	e := newReportEnv(t)
	e.orders.EXPECT().OrderLines(anyArg, garageID, orderID).Return([]usecase.ProfitLine{{RepairOrderID: orderID, LineID: "l1", Revenue: 500}}, nil).Once()
	e.costs.EXPECT().IssueCosts(anyArg, garageID, []string{orderID}).Return(nil, nil).Once()

	resp, err := e.srv.GetRepairOrderProfit(withCost(t.Context()), &reportingv1.GetRepairOrderProfitRequest{GarageId: garageID, RepairOrderId: orderID})
	require.NoError(t, err)
	assert.True(t, resp.GetCostKnown(), "an order with nothing to issue has a known cost")
	assert.Equal(t, int64(500), resp.GetProfit())

	_, err = e.srv.GetRepairOrderProfit(withoutCost(t.Context()), &reportingv1.GetRepairOrderProfitRequest{GarageId: garageID, RepairOrderId: orderID})
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	e.orders.EXPECT().OrderLines(anyArg, garageID, orderID).Return(nil, usecase.ErrOrderNotFound).Once()

	_, err = e.srv.GetRepairOrderProfit(withCost(t.Context()), &reportingv1.GetRepairOrderProfitRequest{GarageId: garageID, RepairOrderId: orderID})
	assert.Equal(t, codes.NotFound, status.Code(err))
}
