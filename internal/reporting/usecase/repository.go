// Package usecase serves the Reporting module's one report (FRD 3.15):
// revenue by day/month/quarter/year for a garage (FR-RPT-01). It owns no
// tables of its own -- revenue is aggregated fresh from Billing &
// Settlement's settlements on every request, through a narrow reader
// interface implemented by an adapter in cmd/server, the same
// cross-module decoupling pattern used everywhere else (HLD Section 2).
package usecase

import (
	"context"
	"errors"
	"time"
)

// Period is the bucket granularity of a revenue report (FR-RPT-01).
type Period string

// Valid Period values.
const (
	PeriodDay     Period = "day"
	PeriodMonth   Period = "month"
	PeriodQuarter Period = "quarter"
	PeriodYear    Period = "year"
)

// IsValid reports whether p is one of the defined Period values.
func (p Period) IsValid() bool {
	switch p {
	case PeriodDay, PeriodMonth, PeriodQuarter, PeriodYear:
		return true
	default:
		return false
	}
}

// RevenuePoint is one bucket of a revenue report, already labeled for its
// requested granularity (e.g. "2026-09-30", "2026-09", "2026-Q3", "2026").
type RevenuePoint struct {
	PeriodLabel string
	Amount      int64
}

// SettlementReader reads a garage's settlement revenue, bucketed by
// period in timezone, an IANA zone name (a day, month, quarter or year is
// the garage's, not UTC's); implemented by an adapter over the Billing &
// Settlement module in cmd/server. from (inclusive) and to (exclusive) are
// calendar dates of that zone: only their year, month and day are read.
type SettlementReader interface {
	RevenueByPeriod(ctx context.Context, garageID string, period Period, timezone string, from, to time.Time) ([]RevenuePoint, error)
}

// GarageReader reads a garage's IANA time zone (FR-TEN-05); implemented by
// an adapter over the Tenant module in cmd/server.
type GarageReader interface {
	Timezone(ctx context.Context, garageID string) (string, error)
}

// Sentinel errors for this module's own validation.
var (
	// ErrInvalidPeriod is returned for a period other than day/month/quarter/year.
	ErrInvalidPeriod = errors.New("period must be day, month, quarter or year")
	// ErrInvalidDateRange is returned when from is not before to.
	ErrInvalidDateRange = errors.New("from must be before to")
)
