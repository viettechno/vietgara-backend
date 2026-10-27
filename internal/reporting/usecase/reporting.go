package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Deps are the dependencies of ReportingUseCase.
type Deps struct {
	Settlements SettlementReader
	Garages     GarageReader
}

// ReportingUseCase implements the Reporting module's revenue report.
type ReportingUseCase struct {
	Deps
}

// NewReportingUseCase wires the use case.
func NewReportingUseCase(deps Deps) *ReportingUseCase {
	return &ReportingUseCase{Deps: deps}
}

// GetRevenueReportRequest is the input for ReportingUseCase.GetRevenueReport.
type GetRevenueReportRequest struct {
	GarageID string
	Period   Period
	// From is inclusive, To is exclusive. They are calendar dates: only
	// their year, month and day are read, as days of the garage's time zone.
	From, To time.Time
}

// RevenueReport is a garage's revenue, bucketed by period, over a date
// range.
type RevenueReport struct {
	Points      []RevenuePoint
	TotalAmount int64
}

// GetRevenueReport returns a garage's revenue by period (FR-RPT-01): the
// Web Admin Dashboard's revenue chart and KPI card (UI Design Spec 3.1).
func (uc *ReportingUseCase) GetRevenueReport(ctx context.Context, req GetRevenueReportRequest) (*RevenueReport, error) {
	if !req.Period.IsValid() {
		return nil, toAppError("get revenue report", ErrInvalidPeriod)
	}

	if !req.From.Before(req.To) {
		return nil, toAppError("get revenue report", ErrInvalidDateRange)
	}

	timezone, err := uc.Garages.Timezone(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError("get revenue report", err)
	}

	// The time zone is validated when it is saved (FR-TEN-05).
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, toAppError("get revenue report", fmt.Errorf("garage time zone %q: %w", timezone, err))
	}

	from, to := localMidnight(req.From, loc), localMidnight(req.To, loc)

	points, err := uc.Settlements.RevenueByPeriod(ctx, req.GarageID, req.Period, loc, from, to)
	if err != nil {
		return nil, toAppError("get revenue report", err)
	}

	var total int64
	for _, p := range points {
		total += p.Amount
	}

	return &RevenueReport{Points: points, TotalAmount: total}, nil
}

// localMidnight is the start of date's calendar day in loc.
func localMidnight(date time.Time, loc *time.Location) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
}

// toAppError classifies err into an *apperror.AppError; op names the
// failed operation for unexpected (internal) errors. This module has only
// two sentinel errors of its own, both request-validation failures.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	switch {
	case errors.Is(err, ErrInvalidPeriod), errors.Is(err, ErrInvalidDateRange):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err)
	default:
		return apperror.Wrap(apperror.CodeInternal, op, err)
	}
}
