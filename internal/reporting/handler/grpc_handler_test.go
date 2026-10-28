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
	garages     *mocks.MockReportingGarageReader
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{settlements: mocks.NewMockReportingSettlementReader(t), garages: mocks.NewMockReportingGarageReader(t)}
	e.garages.EXPECT().Timezone(anyArg, garageID).Return("UTC", nil).Maybe()
	uc := usecase.NewReportingUseCase(usecase.Deps{Settlements: e.settlements, Garages: e.garages})
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
			RevenueByPeriod(anyArg, garageID, usecase.PeriodDay, anyArg, sameInstant(date(t, "2026-09-01")), sameInstant(date(t, "2026-10-01"))).
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

	t.Run("usecase error is converted", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.settlements.EXPECT().
			RevenueByPeriod(anyArg, garageID, usecase.PeriodDay, anyArg, sameInstant(date(t, "2026-09-01")), sameInstant(date(t, "2026-10-01"))).
			Return(nil, assert.AnError).Once()

		_, err := e.srv.GetRevenueReport(t.Context(), &reportingv1.GetRevenueReportRequest{
			GarageId: garageID, Period: reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, From: "2026-09-01", To: "2026-10-01",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}

func TestRequestFromProto(t *testing.T) {
	t.Parallel()

	t.Run("valid request", func(t *testing.T) {
		req := &reportingv1.GetRevenueReportRequest{
			GarageId: garageID,
			Period:   reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY,
			From:     "2026-09-01",
			To:       "2026-10-01",
		}
		in, err := handler.RequestFromProtoForTest(req)
		require.NoError(t, err)
		assert.Equal(t, garageID, in.GarageID)
		assert.Equal(t, usecase.PeriodDay, in.Period)
		assert.Equal(t, date(t, "2026-09-01"), in.From)
		assert.Equal(t, date(t, "2026-10-01"), in.To)
	})

	t.Run("invalid from date", func(t *testing.T) {
		req := &reportingv1.GetRevenueReportRequest{
			GarageId: garageID,
			Period:   reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY,
			From:     "invalid",
			To:       "2026-10-01",
		}
		_, err := handler.RequestFromProtoForTest(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from must be a valid date")
	})

	t.Run("invalid to date", func(t *testing.T) {
		req := &reportingv1.GetRevenueReportRequest{
			GarageId: garageID,
			Period:   reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY,
			From:     "2026-09-01",
			To:       "invalid",
		}
		_, err := handler.RequestFromProtoForTest(req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "to must be a valid date")
	})

	t.Run("unspecified period becomes empty", func(t *testing.T) {
		req := &reportingv1.GetRevenueReportRequest{
			GarageId: garageID,
			Period:   reportingv1.RevenuePeriod_REVENUE_PERIOD_UNSPECIFIED,
			From:     "2026-09-01",
			To:       "2026-10-01",
		}
		in, err := handler.RequestFromProtoForTest(req)
		require.NoError(t, err)
		assert.Equal(t, usecase.Period(""), in.Period)
	})
}

func TestPeriodFromProto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		period   reportingv1.RevenuePeriod
		expected usecase.Period
	}{
		{"day", reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, usecase.PeriodDay},
		{"month", reportingv1.RevenuePeriod_REVENUE_PERIOD_MONTH, usecase.PeriodMonth},
		{"quarter", reportingv1.RevenuePeriod_REVENUE_PERIOD_QUARTER, usecase.PeriodQuarter},
		{"year", reportingv1.RevenuePeriod_REVENUE_PERIOD_YEAR, usecase.PeriodYear},
		{"unspecified", reportingv1.RevenuePeriod_REVENUE_PERIOD_UNSPECIFIED, ""},
		{"unknown", reportingv1.RevenuePeriod(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.PeriodFromProtoForTest(tt.period)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReportToProto(t *testing.T) {
	t.Parallel()

	t.Run("converts report to proto", func(t *testing.T) {
		report := &usecase.RevenueReport{
			Points: []usecase.RevenuePoint{
				{PeriodLabel: "2026-09-01", Amount: 1_000_000},
				{PeriodLabel: "2026-09-02", Amount: 2_000_000},
			},
			TotalAmount: 3_000_000,
		}
		resp := handler.ReportToProtoForTest(report)
		require.NotNil(t, resp)
		assert.Len(t, resp.Points, 2)
		assert.Equal(t, int64(1_000_000), resp.Points[0].Amount)
		assert.Equal(t, int64(2_000_000), resp.Points[1].Amount)
		assert.Equal(t, int64(3_000_000), resp.TotalAmount)
	})

	t.Run("empty report", func(t *testing.T) {
		report := &usecase.RevenueReport{
			Points:      []usecase.RevenuePoint{},
			TotalAmount: 0,
		}
		resp := handler.ReportToProtoForTest(report)
		require.NotNil(t, resp)
		assert.Empty(t, resp.Points)
		assert.Equal(t, int64(0), resp.TotalAmount)
	})
}

func date(t *testing.T, s string) time.Time {
	t.Helper()

	parsed, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)

	return parsed
}

// sameInstant matches a time.Time equal to want, whatever its location.
func sameInstant(want time.Time) any {
	return mock.MatchedBy(func(got time.Time) bool { return got.Equal(want) })
}
