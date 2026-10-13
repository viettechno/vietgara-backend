package handler

import (
	"time"

	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"

	"github.com/viettechno/vietgara-backend/internal/reporting/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const dateLayout = "2006-01-02"

func requestFromProto(req *reportingv1.GetRevenueReportRequest) (usecase.GetRevenueReportRequest, error) {
	from, err := time.Parse(dateLayout, req.GetFrom())
	if err != nil {
		return usecase.GetRevenueReportRequest{}, apperror.Validation("from", apperror.IssueInvalid, "from must be a valid date (YYYY-MM-DD)")
	}

	to, err := time.Parse(dateLayout, req.GetTo())
	if err != nil {
		return usecase.GetRevenueReportRequest{}, apperror.Validation("to", apperror.IssueInvalid, "to must be a valid date (YYYY-MM-DD)")
	}

	return usecase.GetRevenueReportRequest{
		GarageID: req.GetGarageId(), Period: periodFromProto(req.GetPeriod()), From: from, To: to,
	}, nil
}

func periodFromProto(p reportingv1.RevenuePeriod) usecase.Period {
	switch p {
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY:
		return usecase.PeriodDay
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_MONTH:
		return usecase.PeriodMonth
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_QUARTER:
		return usecase.PeriodQuarter
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_YEAR:
		return usecase.PeriodYear
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func reportToProto(report *usecase.RevenueReport) *reportingv1.GetRevenueReportResponse {
	points := make([]*reportingv1.RevenuePoint, 0, len(report.Points))
	for _, p := range report.Points {
		points = append(points, &reportingv1.RevenuePoint{PeriodLabel: p.PeriodLabel, Amount: p.Amount})
	}

	return &reportingv1.GetRevenueReportResponse{Points: points, TotalAmount: report.TotalAmount}
}

// Test helper functions

// RequestFromProtoForTest is a test helper for requestFromProto.
func RequestFromProtoForTest(req *reportingv1.GetRevenueReportRequest) (usecase.GetRevenueReportRequest, error) {
	return requestFromProto(req)
}

// PeriodFromProtoForTest is a test helper for periodFromProto.
func PeriodFromProtoForTest(p reportingv1.RevenuePeriod) usecase.Period {
	return periodFromProto(p)
}

// ReportToProtoForTest is a test helper for reportToProto.
func ReportToProtoForTest(report *usecase.RevenueReport) *reportingv1.GetRevenueReportResponse {
	return reportToProto(report)
}
