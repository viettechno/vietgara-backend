package handler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/reporting/handler"
	"github.com/viettechno/vietgara-backend/internal/reporting/usecase"
)

const garageID = "11111111-1111-1111-1111-111111111111"

var anyArg = mock.Anything

type env struct {
	srv         *handler.Server
	settlements *mocks.MockReportingSettlementReader
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{settlements: mocks.NewMockReportingSettlementReader(t)}
	uc := usecase.NewReportingUseCase(usecase.Deps{Settlements: e.settlements})
	e.srv = handler.NewServer(uc)

	return e
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 1)
	assert.NotNil(t, p[reportingv1.ReportingService_GetRevenueReport_FullMethodName].Garage)
}

func TestGetRevenueReport(t *testing.T) {
	t.Parallel()

	t.Run("returns revenue by day", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.settlements.EXPECT().
			RevenueByPeriod(anyArg, garageID, usecase.PeriodDay, date(t, "2026-09-01"), date(t, "2026-10-01")).
			Return([]usecase.RevenuePoint{{PeriodLabel: "2026-09-30", Amount: 1_000_000}}, nil).Once()

		resp, err := e.srv.GetRevenueReport(t.Context(), &reportingv1.GetRevenueReportRequest{
			GarageId: garageID, Period: reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, From: "2026-09-01", To: "2026-10-01",
		})
		require.NoError(t, err)
		assert.Equal(t, int64(1_000_000), resp.GetTotalAmount())
		assert.Len(t, resp.GetPoints(), 1)
		assert.Equal(t, "2026-09-30", resp.GetPoints()[0].GetPeriodLabel())
	})

	t.Run("invalid date is a validation error", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		_, err := e.srv.GetRevenueReport(t.Context(), &reportingv1.GetRevenueReportRequest{
			GarageId: garageID, Period: reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, From: "not-a-date", To: "2026-10-01",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("invalid period is a validation error", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		_, err := e.srv.GetRevenueReport(t.Context(), &reportingv1.GetRevenueReportRequest{
			GarageId: garageID, Period: reportingv1.RevenuePeriod_REVENUE_PERIOD_UNSPECIFIED, From: "2026-09-01", To: "2026-10-01",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func date(t *testing.T, s string) time.Time {
	t.Helper()

	parsed, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)

	return parsed
}
