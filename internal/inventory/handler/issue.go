package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// GetRepairOrderStockIssue returns the repair order's stock issue (FR-INV-20).
func (s *Server) GetRepairOrderStockIssue(ctx context.Context, req *inventoryv1.GetRepairOrderStockIssueRequest) (*inventoryv1.GetRepairOrderStockIssueResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetRepairOrderStockIssueResponse, error) {
		issue, err := s.uc.GetRepairOrderStockIssue(ctx, req.GetGarageId(), req.GetRepairOrderId())
		if err != nil {
			return nil, err
		}

		return &inventoryv1.GetRepairOrderStockIssueResponse{StockIssue: issueToProto(ctx, issue)}, nil
	})
}

// PostStockIssue posts a draft stock issue now.
func (s *Server) PostStockIssue(ctx context.Context, req *inventoryv1.PostStockIssueRequest) (*inventoryv1.PostStockIssueResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.PostStockIssueResponse, error) {
		issue, err := s.uc.PostStockIssue(ctx, req.GetGarageId(), req.GetStockIssueId(), actorID)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.PostStockIssueResponse{StockIssue: issueToProto(ctx, issue)}, nil
	})
}

func issueToProto(ctx context.Context, i *domain.StockIssue) *inventoryv1.StockIssue {
	seeCost := canSeeCost(ctx)

	out := &inventoryv1.StockIssue{
		Id: i.ID, GarageId: i.GarageID, Number: i.Number, RepairOrderId: i.RepairOrderID, RepairOrderNumber: i.RepairOrderNumber,
		Status: issueStatusToProto(i.Status), Lines: make([]*inventoryv1.StockIssueLine, 0, len(i.Lines)),
		PostedAt: converter.TimePtrToProto(i.PostedAt), PostedByName: i.PostedByName,
		CreatedAt: converter.TimeToProto(i.CreatedAt), UpdatedAt: converter.TimeToProto(i.UpdatedAt), IsShort: i.IsShort,
	}

	for _, l := range i.Lines {
		line := &inventoryv1.StockIssueLine{
			Id: l.ID, RepairOrderLineId: l.RepairOrderLineID, PartId: l.PartID, PartSku: l.PartSKU, PartName: l.PartName,
			BaseUnitAbbreviation: l.BaseUnitAbbreviation, UnitId: l.UnitID, UnitAbbreviation: l.UnitAbbreviation,
			EnteredQuantity: l.EnteredQuantity, Quantity: l.Quantity, ReturnedQuantity: l.ReturnedQuantity, ShortQuantity: l.ShortQuantity,
		}

		if seeCost {
			line.UnitCost = wholeDongRef(l.UnitCost)
		}

		out.Lines = append(out.Lines, line)
	}

	return out
}

func issueStatusToProto(s domain.StockIssueStatus) inventoryv1.StockIssueStatus {
	switch s {
	case domain.StockIssueDraft:
		return inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_DRAFT
	case domain.StockIssuePosted:
		return inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_POSTED
	case domain.StockIssueCancelled:
		return inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_CANCELLED
	default:
		return inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_UNSPECIFIED
	}
}
