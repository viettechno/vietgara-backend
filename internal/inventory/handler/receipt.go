package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// The paths an UpdateGoodsReceipt mask may name.
const (
	pathDefaultSupplier   = "default_supplier_id"
	pathReceiptDate       = "receipt_date"
	pathDeliveryNote      = "delivery_note_number"
	pathNotes             = "notes"
	pathLines             = "lines"
	fieldUpdateMaskReason = "updateMask"
)

// ListGoodsReceipts lists the garage's goods receipts (FR-INV-06).
func (s *Server) ListGoodsReceipts(ctx context.Context, req *inventoryv1.ListGoodsReceiptsRequest) (*inventoryv1.ListGoodsReceiptsResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListGoodsReceiptsResponse, error) {
		page, err := s.uc.ListGoodsReceipts(ctx, usecase.ListGoodsReceiptsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(), Query: req.GetQuery(),
			SupplierID: req.GetSupplierId(), Status: receiptStatusFromProto(req.GetStatus()),
		})
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.ListGoodsReceiptsResponse{
			Data: make([]*inventoryv1.GoodsReceipt, 0, len(page.Items)), Pagination: paginationToProto(page),
		}
		for _, r := range page.Items {
			resp.Data = append(resp.Data, receiptToProto(ctx, r))
		}

		return resp, nil
	})
}

// GetGoodsReceipt returns one goods receipt with its lines.
func (s *Server) GetGoodsReceipt(ctx context.Context, req *inventoryv1.GetGoodsReceiptRequest) (*inventoryv1.GetGoodsReceiptResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetGoodsReceiptResponse, error) {
		r, err := s.uc.GetGoodsReceipt(ctx, req.GetGarageId(), req.GetGoodsReceiptId())
		if err != nil {
			return nil, err
		}

		return &inventoryv1.GetGoodsReceiptResponse{GoodsReceipt: receiptToProto(ctx, r)}, nil
	})
}

// CreateGoodsReceipt saves a new draft goods receipt.
func (s *Server) CreateGoodsReceipt(ctx context.Context, req *inventoryv1.CreateGoodsReceiptRequest) (*inventoryv1.CreateGoodsReceiptResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CreateGoodsReceiptResponse, error) {
		in := req.GetGoodsReceipt()

		r, err := s.uc.CreateGoodsReceipt(ctx, usecase.CreateGoodsReceiptRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, CanSetCost: canSeeCost(ctx), DefaultSupplierID: in.GetDefaultSupplierId(),
			ReceiptDate: in.GetReceiptDate(), DeliveryNoteNumber: in.GetDeliveryNoteNumber(), Notes: in.GetNotes(),
			Lines: receiptLinesFromProto(in.GetLines()),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CreateGoodsReceiptResponse{GoodsReceipt: receiptToProto(ctx, r)}, nil
	})
}

// UpdateGoodsReceipt changes a draft goods receipt.
func (s *Server) UpdateGoodsReceipt(ctx context.Context, req *inventoryv1.UpdateGoodsReceiptRequest) (*inventoryv1.UpdateGoodsReceiptResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.UpdateGoodsReceiptResponse, error) {
		in := req.GetGoodsReceipt()
		update := usecase.UpdateGoodsReceiptRequest{
			GarageID: req.GetGarageId(), ReceiptID: req.GetGoodsReceiptId(), ActorID: actorID, CanSetCost: canSeeCost(ctx),
		}

		paths := req.GetUpdateMask().GetPaths()
		if len(paths) == 0 {
			paths = presentReceiptFields(in)
		}

		for _, path := range paths {
			switch path {
			case pathDefaultSupplier:
				update.SetDefaultSupplier, update.DefaultSupplierID = true, in.GetDefaultSupplierId()
			case pathReceiptDate:
				update.ReceiptDate = new(in.GetReceiptDate())
			case pathDeliveryNote:
				update.DeliveryNoteNumber = new(in.GetDeliveryNoteNumber())
			case pathNotes:
				update.Notes = new(in.GetNotes())
			case pathLines:
				update.Lines = new(receiptLinesFromProto(in.GetLines()))
			default:
				return nil, apperror.Validation(fieldUpdateMaskReason, apperror.IssueInvalid, "field "+path+" cannot be updated")
			}
		}

		r, err := s.uc.UpdateGoodsReceipt(ctx, update)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.UpdateGoodsReceiptResponse{GoodsReceipt: receiptToProto(ctx, r)}, nil
	})
}

// CompleteGoodsReceipt posts the receipt's stock movements.
func (s *Server) CompleteGoodsReceipt(ctx context.Context, req *inventoryv1.CompleteGoodsReceiptRequest) (*inventoryv1.CompleteGoodsReceiptResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CompleteGoodsReceiptResponse, error) {
		r, err := s.uc.CompleteGoodsReceipt(ctx, req.GetGarageId(), req.GetGoodsReceiptId(), actorID)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CompleteGoodsReceiptResponse{GoodsReceipt: receiptToProto(ctx, r)}, nil
	})
}

// CancelGoodsReceipt closes a draft or reverses a completed receipt.
func (s *Server) CancelGoodsReceipt(ctx context.Context, req *inventoryv1.CancelGoodsReceiptRequest) (*inventoryv1.CancelGoodsReceiptResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CancelGoodsReceiptResponse, error) {
		r, err := s.uc.CancelGoodsReceipt(ctx, req.GetGarageId(), req.GetGoodsReceiptId(), actorID)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CancelGoodsReceiptResponse{GoodsReceipt: receiptToProto(ctx, r)}, nil
	})
}

// presentReceiptFields lists the fields set on a receipt, for a request whose
// mask the client left out.
func presentReceiptFields(in *inventoryv1.GoodsReceipt) []string {
	var paths []string

	for path, set := range map[string]bool{
		pathDefaultSupplier: in.DefaultSupplierId != nil, pathReceiptDate: in.GetReceiptDate() != "",
		pathDeliveryNote: in.GetDeliveryNoteNumber() != "", pathNotes: in.GetNotes() != "", pathLines: len(in.GetLines()) > 0,
	} {
		if set {
			paths = append(paths, path)
		}
	}

	return paths
}

func receiptLinesFromProto(lines []*inventoryv1.GoodsReceiptLine) []usecase.ReceiptLineInput {
	out := make([]usecase.ReceiptLineInput, 0, len(lines))

	for _, l := range lines {
		in := usecase.ReceiptLineInput{
			ID: l.GetId(), PartID: l.GetPartId(), EnteredQuantity: l.GetEnteredQuantity(), SupplierID: l.GetSupplierId(),
			UnitCost: l.GetUnitCost(),
		}
		if l.UnitId != nil {
			in.UnitID = *l.UnitId
		}

		out = append(out, in)
	}

	return out
}

func receiptToProto(ctx context.Context, r *domain.GoodsReceipt) *inventoryv1.GoodsReceipt {
	seeCost := canSeeCost(ctx)

	out := &inventoryv1.GoodsReceipt{
		Id: r.ID, GarageId: r.GarageID, Number: r.Number, Status: receiptStatusToProto(r.Status),
		DefaultSupplierId: r.DefaultSupplierID, DefaultSupplierName: r.DefaultSupplierName, ReceiptDate: r.ReceiptDate,
		DeliveryNoteNumber: r.DeliveryNoteNumber, Notes: r.Notes, Lines: make([]*inventoryv1.GoodsReceiptLine, 0, len(r.Lines)),
		CompletedAt: converter.TimePtrToProto(r.CompletedAt), CancelledAt: converter.TimePtrToProto(r.CancelledAt),
		CreatedAt: converter.TimeToProto(r.CreatedAt), UpdatedAt: converter.TimeToProto(r.UpdatedAt),
	}

	if seeCost {
		out.TotalAmount = &r.TotalAmount
	}

	for i := range r.Lines {
		l := &r.Lines[i]
		line := &inventoryv1.GoodsReceiptLine{
			Id: l.ID, PartId: l.PartID, PartSku: l.PartSKU, PartName: l.PartName, BaseUnitAbbreviation: l.BaseUnitAbbreviation,
			UnitId: l.UnitID, UnitAbbreviation: l.UnitAbbreviation, EnteredQuantity: l.EnteredQuantity, Quantity: l.Quantity,
			SupplierName: l.SupplierName,
		}

		if l.SupplierID != nil {
			line.SupplierId = *l.SupplierID
		}

		if seeCost {
			line.UnitCost, line.LineAmount = &l.UnitCost, new(l.Amount())
		}

		out.Lines = append(out.Lines, line)
	}

	return out
}

func receiptStatusToProto(s domain.GoodsReceiptStatus) inventoryv1.GoodsReceiptStatus {
	switch s {
	case domain.GoodsReceiptDraft:
		return inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_DRAFT
	case domain.GoodsReceiptCompleted:
		return inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_COMPLETED
	case domain.GoodsReceiptCancelled:
		return inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_CANCELLED
	default:
		return inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_UNSPECIFIED
	}
}

// receiptStatusFromProto maps UNSPECIFIED (and unknown values) to "", which
// the use case treats as "every status" when listing.
func receiptStatusFromProto(s inventoryv1.GoodsReceiptStatus) domain.GoodsReceiptStatus {
	switch s {
	case inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_DRAFT:
		return domain.GoodsReceiptDraft
	case inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_COMPLETED:
		return domain.GoodsReceiptCompleted
	case inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_CANCELLED:
		return domain.GoodsReceiptCancelled
	case inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}
