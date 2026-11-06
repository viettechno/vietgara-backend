package handler

import (
	"context"
	"time"

	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/reporting/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// GetTechnicianReport serves the technician performance report (FR-RPT-02).
func (s *Server) GetTechnicianReport(ctx context.Context, req *reportingv1.GetTechnicianReportRequest) (*reportingv1.GetTechnicianReportResponse, error) {
	from, to, err := datesOf(req.GetFrom(), req.GetTo())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	list, err := s.uc.GetTechnicianReport(ctx, usecase.GetTechnicianReportRequest{GarageID: req.GetGarageId(), From: from, To: to})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &reportingv1.GetTechnicianReportResponse{Technicians: make([]*reportingv1.TechnicianPerformance, 0, len(list))}
	for _, t := range list {
		resp.Technicians = append(resp.Technicians, &reportingv1.TechnicianPerformance{
			TechnicianId: t.TechnicianID, TechnicianName: t.TechnicianName, CompletedLines: toInt32(t.CompletedLines),
			RepairOrders: toInt32(t.RepairOrders), AverageTurnaroundMinutes: t.AverageTurnaroundMinutes,
		})
	}

	return resp, nil
}

// GetProfitReport serves the profit report (FR-RPT-05). Every figure reveals
// the cost of parts, so it also needs inventory.cost.read.
func (s *Server) GetProfitReport(ctx context.Context, req *reportingv1.GetProfitReportRequest) (*reportingv1.GetProfitReportResponse, error) {
	if err := requireCost(ctx); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	from, to, err := datesOf(req.GetFrom(), req.GetTo())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	report, err := s.uc.GetProfitReport(ctx, usecase.GetProfitReportRequest{
		GarageID: req.GetGarageId(), GroupBy: groupByFromProto(req.GetGroupBy()), From: from, To: to,
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &reportingv1.GetProfitReportResponse{
		Rows:         make([]*reportingv1.ProfitRow, 0, len(report.Rows)),
		TotalRevenue: report.TotalRevenue, TotalCost: report.TotalCost, TotalProfit: report.TotalProfit,
		OrdersWithoutCost: toInt32(report.OrdersWithoutCost),
	}

	for _, r := range report.Rows {
		resp.Rows = append(resp.Rows, &reportingv1.ProfitRow{
			Key: r.Key, Label: r.Label, RepairOrders: toInt32(r.RepairOrders), Revenue: r.Revenue, Cost: r.Cost, Profit: r.Profit,
		})
	}

	return resp, nil
}

// GetRepairOrderProfit serves the profit on a repair order (FR-RPT-05).
func (s *Server) GetRepairOrderProfit(ctx context.Context, req *reportingv1.GetRepairOrderProfitRequest) (*reportingv1.RepairOrderProfit, error) {
	if err := requireCost(ctx); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	profit, err := s.uc.GetRepairOrderProfit(ctx, req.GetGarageId(), req.GetRepairOrderId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &reportingv1.RepairOrderProfit{Revenue: profit.Revenue, Cost: profit.Cost, Profit: profit.Profit, CostKnown: profit.CostKnown}, nil
}

// requireCost refuses a caller without inventory.cost.read: the policy checks
// the first permission of a report, this the second.
func requireCost(ctx context.Context) error {
	if a, ok := access.FromContext(ctx); ok && a.Has(access.InventoryCostRead) {
		return nil
	}

	return apperror.New(apperror.CodePermissionDenied, "inventory.cost.read is required").WithReason(apperror.ReasonPermissionDenied)
}

func datesOf(from, to string) (time.Time, time.Time, error) {
	f, err := time.Parse(dateLayout, from)
	if err != nil {
		return time.Time{}, time.Time{}, apperror.Validation("from", apperror.IssueInvalid, "from must be a valid date (YYYY-MM-DD)")
	}

	t, err := time.Parse(dateLayout, to)
	if err != nil {
		return time.Time{}, time.Time{}, apperror.Validation("to", apperror.IssueInvalid, "to must be a valid date (YYYY-MM-DD)")
	}

	return f, t, nil
}

func groupByFromProto(g reportingv1.ProfitGroupBy) usecase.ProfitGroupBy {
	switch g {
	case reportingv1.ProfitGroupBy_PROFIT_GROUP_BY_REPAIR_ORDER:
		return usecase.GroupByRepairOrder
	case reportingv1.ProfitGroupBy_PROFIT_GROUP_BY_JOB:
		return usecase.GroupByJob
	case reportingv1.ProfitGroupBy_PROFIT_GROUP_BY_TECHNICIAN:
		return usecase.GroupByTechnician
	case reportingv1.ProfitGroupBy_PROFIT_GROUP_BY_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func toInt32(n int) int32 {
	return int32(min(max(n, 0), 1<<31-1)) //nolint:gosec // clamped
}
