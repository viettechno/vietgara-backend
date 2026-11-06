package usecase

import (
	"context"
	"sort"
	"time"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// maxReportDays is the longest range a technician or profit report accepts.
const maxReportDays = 366

// TechnicianPerformance is one technician's work in a period (FR-RPT-02).
type TechnicianPerformance struct {
	TechnicianID             string
	TechnicianName           string
	CompletedLines           int
	RepairOrders             int
	AverageTurnaroundMinutes int64
}

// GetTechnicianReportRequest is the input of GetTechnicianReport.
type GetTechnicianReportRequest struct {
	GarageID string
	// From is inclusive, To exclusive; calendar dates of the garage's zone.
	From, To time.Time
}

// checkRange validates the dates of a report: from before to, and no more than
// maxReportDays apart. It names the field at fault.
func checkRange(from, to time.Time) error {
	if !from.Before(to) {
		return apperror.Validation("from", apperror.IssueInvalid, ErrInvalidDateRange.Error())
	}

	if to.Sub(from) > maxReportDays*24*time.Hour {
		return apperror.Validation("to", apperror.IssueInvalid, "the range is more than 366 days")
	}

	return nil
}

// GetTechnicianReport counts, per technician assigned to a line, the lines
// completed in the period, the repair orders they belong to and the mean time
// from start to completion; the technician with the most completed lines
// first (FR-RPT-02).
func (uc *ReportingUseCase) GetTechnicianReport(ctx context.Context, req GetTechnicianReportRequest) ([]TechnicianPerformance, error) {
	const op = "get technician report"

	from, to := calendarDate(req.From), calendarDate(req.To)
	if err := checkRange(from, to); err != nil {
		return nil, err
	}

	timezone, err := uc.Garages.Timezone(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	work, err := uc.Work.CompletedWork(ctx, req.GarageID, timezone, from, to)
	if err != nil {
		return nil, toAppError(op, err)
	}

	ids := make([]string, len(work))
	for i, w := range work {
		ids[i] = w.TechnicianID
	}

	names := map[string]string{}

	if len(ids) > 0 {
		if names, err = uc.Names.UserNames(ctx, ids); err != nil {
			return nil, toAppError(op, err)
		}
	}

	out := make([]TechnicianPerformance, len(work))
	for i, w := range work {
		out[i] = TechnicianPerformance{
			TechnicianID: w.TechnicianID, TechnicianName: names[w.TechnicianID], CompletedLines: w.CompletedLines,
			RepairOrders: w.RepairOrders, AverageTurnaroundMinutes: w.AverageTurnaroundMinutes,
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		switch {
		case out[i].CompletedLines != out[j].CompletedLines:
			return out[i].CompletedLines > out[j].CompletedLines
		case out[i].TechnicianName != out[j].TechnicianName:
			return out[i].TechnicianName < out[j].TechnicianName
		default:
			return out[i].TechnicianID < out[j].TechnicianID
		}
	})

	return out, nil
}
