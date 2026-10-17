package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// The paths an UpdateStockCount mask may name (notes is shared with the
// goods receipt, see pathNotes).
const pathCountName = "name"

// ListStockCounts lists the garage's stock counts (FR-INV-12).
func (s *Server) ListStockCounts(ctx context.Context, req *inventoryv1.ListStockCountsRequest) (*inventoryv1.ListStockCountsResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListStockCountsResponse, error) {
		page, err := s.uc.ListStockCounts(ctx, usecase.ListStockCountsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(), Query: req.GetQuery(),
			Status: countStatusFromProto(req.GetStatus()),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.ListStockCountsResponse{
			Data:       mapPage(page, func(c *domain.StockCount) *inventoryv1.StockCount { return countToProto(ctx, c) }),
			Pagination: paginationToProto(page),
		}, nil
	})
}

// GetStockCount returns one stock count with its lines.
func (s *Server) GetStockCount(ctx context.Context, req *inventoryv1.GetStockCountRequest) (*inventoryv1.GetStockCountResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetStockCountResponse, error) {
		c, err := s.uc.GetStockCount(ctx, req.GetGarageId(), req.GetStockCountId())
		if err != nil {
			return nil, err
		}

		return &inventoryv1.GetStockCountResponse{StockCount: countToProto(ctx, c)}, nil
	})
}

// CreateStockCount saves a new draft stock count.
func (s *Server) CreateStockCount(ctx context.Context, req *inventoryv1.CreateStockCountRequest) (*inventoryv1.CreateStockCountResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CreateStockCountResponse, error) {
		in := req.GetStockCount()

		c, err := s.uc.CreateStockCount(ctx, usecase.CreateStockCountRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, Name: in.GetName(), Notes: in.GetNotes(),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CreateStockCountResponse{StockCount: countToProto(ctx, c)}, nil
	})
}

// UpdateStockCount changes the name and notes of a draft stock count.
func (s *Server) UpdateStockCount(ctx context.Context, req *inventoryv1.UpdateStockCountRequest) (*inventoryv1.UpdateStockCountResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.UpdateStockCountResponse, error) {
		in := req.GetStockCount()
		update := usecase.UpdateStockCountRequest{
			GarageID: req.GetGarageId(), CountID: req.GetStockCountId(), ActorID: actorID,
		}

		paths := req.GetUpdateMask().GetPaths()
		if len(paths) == 0 {
			paths = []string{pathCountName, pathNotes}
		}

		for _, path := range paths {
			switch path {
			case pathCountName:
				update.Name = new(in.GetName())
			case pathNotes:
				update.Notes = new(in.GetNotes())
			default:
				return nil, apperror.Validation(fieldUpdateMaskReason, apperror.IssueInvalid, "field "+path+" cannot be updated")
			}
		}

		c, err := s.uc.UpdateStockCount(ctx, update)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.UpdateStockCountResponse{StockCount: countToProto(ctx, c)}, nil
	})
}

// AddStockCountLines adds parts to a draft stock count.
func (s *Server) AddStockCountLines(ctx context.Context, req *inventoryv1.AddStockCountLinesRequest) (*inventoryv1.AddStockCountLinesResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.AddStockCountLinesResponse, error) {
		c, err := s.uc.AddStockCountLines(ctx, usecase.AddStockCountLinesRequest{
			GarageID: req.GetGarageId(), CountID: req.GetStockCountId(), ActorID: actorID, PartIDs: req.GetPartIds(),
			CategoryID: req.GetCategoryId(),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.AddStockCountLinesResponse{StockCount: countToProto(ctx, c)}, nil
	})
}

// UpdateStockCountLine types the counted quantity of one line.
func (s *Server) UpdateStockCountLine(ctx context.Context, req *inventoryv1.UpdateStockCountLineRequest) (*inventoryv1.UpdateStockCountLineResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.UpdateStockCountLineResponse, error) {
		in := req.GetLine()

		c, err := s.uc.UpdateStockCountLine(ctx, usecase.UpdateStockCountLineRequest{
			GarageID: req.GetGarageId(), CountID: req.GetStockCountId(), LineID: req.GetLineId(), ActorID: actorID,
			UnitID: in.GetUnitId(), EnteredCounted: in.GetEnteredCountedQuantity(),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.UpdateStockCountLineResponse{StockCount: countToProto(ctx, c)}, nil
	})
}

// DeleteStockCountLine removes a line from a draft stock count.
func (s *Server) DeleteStockCountLine(ctx context.Context, req *inventoryv1.DeleteStockCountLineRequest) (*inventoryv1.DeleteStockCountLineResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.DeleteStockCountLineResponse, error) {
		c, err := s.uc.DeleteStockCountLine(ctx, req.GetGarageId(), req.GetStockCountId(), req.GetLineId(), actorID)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.DeleteStockCountLineResponse{StockCount: countToProto(ctx, c)}, nil
	})
}

// MergeStockCounts folds draft stock counts into the first one.
func (s *Server) MergeStockCounts(ctx context.Context, req *inventoryv1.MergeStockCountsRequest) (*inventoryv1.MergeStockCountsResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.MergeStockCountsResponse, error) {
		c, err := s.uc.MergeStockCounts(ctx, usecase.MergeStockCountsRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, CountIDs: req.GetStockCountIds(),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.MergeStockCountsResponse{StockCount: countToProto(ctx, c)}, nil
	})
}

// CompleteStockCount posts the adjustments of a draft stock count.
func (s *Server) CompleteStockCount(ctx context.Context, req *inventoryv1.CompleteStockCountRequest) (*inventoryv1.CompleteStockCountResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CompleteStockCountResponse, error) {
		c, err := s.uc.CompleteStockCount(ctx, usecase.CompleteStockCountRequest{
			GarageID: req.GetGarageId(), CountID: req.GetStockCountId(), ActorID: actorID, SkipUncounted: req.GetSkipUncounted(),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CompleteStockCountResponse{StockCount: countToProto(ctx, c)}, nil
	})
}

// CancelStockCount closes a draft or reverses a completed stock count.
func (s *Server) CancelStockCount(ctx context.Context, req *inventoryv1.CancelStockCountRequest) (*inventoryv1.CancelStockCountResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CancelStockCountResponse, error) {
		c, err := s.uc.CancelStockCount(ctx, req.GetGarageId(), req.GetStockCountId(), actorID)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CancelStockCountResponse{StockCount: countToProto(ctx, c)}, nil
	})
}

func countToProto(ctx context.Context, c *domain.StockCount) *inventoryv1.StockCount {
	out := &inventoryv1.StockCount{
		Id: c.ID, GarageId: c.GarageID, Number: c.Number, Status: countStatusToProto(c.Status), Name: c.Name, Notes: c.Notes,
		Lines:          make([]*inventoryv1.StockCountLine, 0, len(c.Lines)),
		UncountedLines: toInt32(c.Tally.Uncounted), MatchedLines: toInt32(c.Tally.Matched), OverLines: toInt32(c.Tally.Over),
		ShortLines:  toInt32(c.Tally.Short),
		CompletedAt: converter.TimePtrToProto(c.CompletedAt), CompletedByName: c.CompletedByName,
		CancelledAt: converter.TimePtrToProto(c.CancelledAt), CreatedByName: c.CreatedByName,
		CreatedAt: converter.TimeToProto(c.CreatedAt), UpdatedAt: converter.TimeToProto(c.UpdatedAt),
	}

	seeCost := canSeeCost(ctx)
	if seeCost {
		out.TotalDifferenceValue = &c.TotalDifferenceValue
	}

	for i := range c.Lines {
		out.Lines = append(out.Lines, countLineToProto(&c.Lines[i], seeCost))
	}

	return out
}

func countLineToProto(l *domain.StockCountLine, seeCost bool) *inventoryv1.StockCountLine {
	out := &inventoryv1.StockCountLine{
		Id: l.ID, PartId: l.PartID, PartSku: l.PartSKU, PartName: l.PartName, BaseUnitAbbreviation: l.BaseUnitAbbreviation,
		UnitId: l.UnitID, UnitAbbreviation: l.UnitAbbreviation, SystemQuantity: l.SystemQuantity,
		Result: countResultToProto(l.Result()), CountedAt: converter.TimePtrToProto(l.CountedAt),
	}

	if l.EnteredCounted != nil {
		out.EnteredCountedQuantity = *l.EnteredCounted
	}

	if l.Counted != nil {
		out.CountedQuantity = *l.Counted
	}

	if diff, ok := l.Difference(); ok {
		out.DifferenceQuantity = diff
	}

	if seeCost {
		out.AverageCost, out.DifferenceValue = wholeDongRef(l.AverageCost), l.DifferenceValue()
	}

	return out
}

func countStatusToProto(s domain.StockCountStatus) inventoryv1.StockCountStatus {
	switch s {
	case domain.StockCountDraft:
		return inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_DRAFT
	case domain.StockCountCompleted:
		return inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_COMPLETED
	case domain.StockCountCancelled:
		return inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_CANCELLED
	default:
		return inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_UNSPECIFIED
	}
}

// countStatusFromProto maps UNSPECIFIED (and unknown values) to "", which the
// use case treats as "every status" when listing.
func countStatusFromProto(s inventoryv1.StockCountStatus) domain.StockCountStatus {
	switch s {
	case inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_DRAFT:
		return domain.StockCountDraft
	case inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_COMPLETED:
		return domain.StockCountCompleted
	case inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_CANCELLED:
		return domain.StockCountCancelled
	case inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func countResultToProto(r domain.CountResult) inventoryv1.StockCountLineResult {
	switch r {
	case domain.CountUncounted:
		return inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_UNCOUNTED
	case domain.CountMatched:
		return inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_MATCHED
	case domain.CountOver:
		return inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_OVER
	case domain.CountShort:
		return inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_SHORT
	default:
		return inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_UNSPECIFIED
	}
}
