package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/reporting/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestReportingUseCase_GetRevenueReport(t *testing.T) {
	t.Parallel()

	t.Run("sums the points into a total", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		want := []usecase.RevenuePoint{{PeriodLabel: "2026-09-29", Amount: 250_000}, {PeriodLabel: "2026-09-30", Amount: 750_000}}
		d.Settlements.EXPECT().
			RevenueByPeriod(mock.Anything, garageID, usecase.PeriodDay, date("2026-09-01"), date("2026-10-01")).
			Return(want, nil).Once()

		report, err := uc.GetRevenueReport(t.Context(), usecase.GetRevenueReportRequest{
			GarageID: garageID, Period: usecase.PeriodDay, From: date("2026-09-01"), To: date("2026-10-01"),
		})
		require.NoError(t, err)
		require.Equal(t, want, report.Points)
		require.Equal(t, int64(1_000_000), report.TotalAmount)
	})

	t.Run("no settlements in range", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().RevenueByPeriod(mock.Anything, garageID, usecase.PeriodMonth, mock.Anything, mock.Anything).
			Return(nil, nil).Once()

		report, err := uc.GetRevenueReport(t.Context(), usecase.GetRevenueReportRequest{
			GarageID: garageID, Period: usecase.PeriodMonth, From: date("2026-01-01"), To: date("2026-10-01"),
		})
		require.NoError(t, err)
		require.Empty(t, report.Points)
		require.Equal(t, int64(0), report.TotalAmount)
	})

	t.Run("invalid period", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := uc.GetRevenueReport(t.Context(), usecase.GetRevenueReportRequest{
			GarageID: garageID, Period: "week", From: date("2026-09-01"), To: date("2026-10-01"),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("from not before to", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := uc.GetRevenueReport(t.Context(), usecase.GetRevenueReportRequest{
			GarageID: garageID, Period: usecase.PeriodDay, From: date("2026-10-01"), To: date("2026-09-01"),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("reader failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().RevenueByPeriod(mock.Anything, garageID, usecase.PeriodDay, mock.Anything, mock.Anything).
			Return(nil, errBoomReporting).Once()

		_, err := uc.GetRevenueReport(t.Context(), usecase.GetRevenueReportRequest{
			GarageID: garageID, Period: usecase.PeriodDay, From: date("2026-09-01"), To: date("2026-10-01"),
		})
		assertAppError(t, err, apperror.CodeInternal)
	})
}
