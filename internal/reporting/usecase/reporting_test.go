package usecase_test

import (
	"testing"
	"time"

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

		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return(hcm, nil).Once()
		d.Settlements.EXPECT().
			RevenueByPeriod(mock.Anything, garageID, usecase.PeriodDay, hcm, onDate("2026-09-01"), onDate("2026-10-01")).
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
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return(hcm, nil).Once()
		d.Settlements.EXPECT().RevenueByPeriod(mock.Anything, garageID, usecase.PeriodMonth, mock.Anything, mock.Anything, mock.Anything).
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
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return(hcm, nil).Once()
		d.Settlements.EXPECT().RevenueByPeriod(mock.Anything, garageID, usecase.PeriodDay, mock.Anything, mock.Anything, mock.Anything).
			Return(nil, errBoomReporting).Once()

		_, err := uc.GetRevenueReport(t.Context(), usecase.GetRevenueReportRequest{
			GarageID: garageID, Period: usecase.PeriodDay, From: date("2026-09-01"), To: date("2026-10-01"),
		})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("the dates are calendar dates of the garage's zone, whatever zone they carry", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("America/New_York", nil).Once()
		d.Settlements.EXPECT().
			RevenueByPeriod(mock.Anything, garageID, usecase.PeriodDay, "America/New_York", onDate("2026-10-28"), onDate("2026-10-29")).
			Return(nil, nil).Once()

		// Late on the 28th in Vietnam is still the 28th as a calendar date.
		hanoi := time.FixedZone("UTC+7", 7*60*60)
		_, err := uc.GetRevenueReport(t.Context(), usecase.GetRevenueReportRequest{
			GarageID: garageID, Period: usecase.PeriodDay,
			From: time.Date(2026, 10, 28, 23, 0, 0, 0, hanoi), To: date("2026-10-29"),
		})
		require.NoError(t, err)
	})

	t.Run("garage reader failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoomReporting).Once()

		_, err := uc.GetRevenueReport(t.Context(), usecase.GetRevenueReportRequest{
			GarageID: garageID, Period: usecase.PeriodDay, From: date("2026-09-01"), To: date("2026-10-01"),
		})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("the range is checked on the dates, not the times", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		// 10:00 to 20:00 on one day is an empty range of dates.
		_, err := uc.GetRevenueReport(t.Context(), usecase.GetRevenueReportRequest{
			GarageID: garageID, Period: usecase.PeriodDay,
			From: time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 28, 20, 0, 0, 0, time.UTC),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}
