package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// ListWriteOffs lists the garage's write-offs (FR-INV-09).
func (s *Server) ListWriteOffs(ctx context.Context, req *inventoryv1.ListWriteOffsRequest) (*inventoryv1.ListWriteOffsResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListWriteOffsResponse, error) {
		page, err := s.uc.ListWriteOffs(ctx, usecase.ListWriteOffsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(), Query: req.GetQuery(),
			Reason: writeOffReasonFromProto(req.GetReason()), Status: adjustmentStatusFromProto(req.GetStatus()),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.ListWriteOffsResponse{
			Data:       mapPage(page, func(w *domain.WriteOff) *inventoryv1.WriteOff { return writeOffToProto(ctx, w) }),
			Pagination: paginationToProto(page),
		}, nil
	})
}

// GetWriteOff returns one write-off with its lines.
func (s *Server) GetWriteOff(ctx context.Context, req *inventoryv1.GetWriteOffRequest) (*inventoryv1.GetWriteOffResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetWriteOffResponse, error) {
		w, err := s.uc.GetWriteOff(ctx, req.GetGarageId(), req.GetWriteOffId())
		if err != nil {
			return nil, err
		}

		return &inventoryv1.GetWriteOffResponse{WriteOff: writeOffToProto(ctx, w)}, nil
	})
}

// CreateWriteOff posts a write-off.
func (s *Server) CreateWriteOff(ctx context.Context, req *inventoryv1.CreateWriteOffRequest) (*inventoryv1.CreateWriteOffResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CreateWriteOffResponse, error) {
		in := req.GetWriteOff()

		w, err := s.uc.CreateWriteOff(ctx, usecase.CreateWriteOffRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, Reason: writeOffReasonFromProto(in.GetReason()), Notes: in.GetNotes(),
			Lines: writeOffLinesFromProto(in.GetLines()),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CreateWriteOffResponse{WriteOff: writeOffToProto(ctx, w)}, nil
	})
}

// CancelWriteOff puts the written-off parts back in stock.
func (s *Server) CancelWriteOff(ctx context.Context, req *inventoryv1.CancelWriteOffRequest) (*inventoryv1.CancelWriteOffResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CancelWriteOffResponse, error) {
		w, err := s.uc.CancelWriteOff(ctx, req.GetGarageId(), req.GetWriteOffId(), actorID)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CancelWriteOffResponse{WriteOff: writeOffToProto(ctx, w)}, nil
	})
}

// ListSupplierReturns lists the garage's returns to supplier (FR-INV-10).
func (s *Server) ListSupplierReturns(ctx context.Context, req *inventoryv1.ListSupplierReturnsRequest) (*inventoryv1.ListSupplierReturnsResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListSupplierReturnsResponse, error) {
		page, err := s.uc.ListSupplierReturns(ctx, usecase.ListSupplierReturnsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(), Query: req.GetQuery(),
			SupplierID: req.GetSupplierId(), Status: adjustmentStatusFromProto(req.GetStatus()),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.ListSupplierReturnsResponse{
			Data:       mapPage(page, func(r *domain.SupplierReturn) *inventoryv1.SupplierReturn { return supplierReturnToProto(ctx, r) }),
			Pagination: paginationToProto(page),
		}, nil
	})
}

// GetSupplierReturn returns one return to supplier with its lines.
func (s *Server) GetSupplierReturn(ctx context.Context, req *inventoryv1.GetSupplierReturnRequest) (*inventoryv1.GetSupplierReturnResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetSupplierReturnResponse, error) {
		r, err := s.uc.GetSupplierReturn(ctx, req.GetGarageId(), req.GetSupplierReturnId())
		if err != nil {
			return nil, err
		}

		return &inventoryv1.GetSupplierReturnResponse{SupplierReturn: supplierReturnToProto(ctx, r)}, nil
	})
}

// CreateSupplierReturn posts a return to supplier.
func (s *Server) CreateSupplierReturn(ctx context.Context, req *inventoryv1.CreateSupplierReturnRequest) (*inventoryv1.CreateSupplierReturnResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CreateSupplierReturnResponse, error) {
		in := req.GetSupplierReturn()

		r, err := s.uc.CreateSupplierReturn(ctx, usecase.CreateSupplierReturnRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, SupplierID: in.GetSupplierId(), GoodsReceiptID: in.GetGoodsReceiptId(),
			CreditNoteNumber: in.GetCreditNoteNumber(), Notes: in.GetNotes(), CanSetCost: canSeeCost(ctx),
			Lines: supplierReturnLinesFromProto(in.GetLines()),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CreateSupplierReturnResponse{SupplierReturn: supplierReturnToProto(ctx, r)}, nil
	})
}

// CancelSupplierReturn puts the returned parts back in stock.
func (s *Server) CancelSupplierReturn(ctx context.Context, req *inventoryv1.CancelSupplierReturnRequest) (*inventoryv1.CancelSupplierReturnResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CancelSupplierReturnResponse, error) {
		r, err := s.uc.CancelSupplierReturn(ctx, req.GetGarageId(), req.GetSupplierReturnId(), actorID)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.CancelSupplierReturnResponse{SupplierReturn: supplierReturnToProto(ctx, r)}, nil
	})
}

// ReturnRepairOrderPart returns unused parts of a repair order to stock
// (FR-INV-11).
func (s *Server) ReturnRepairOrderPart(ctx context.Context, req *inventoryv1.ReturnRepairOrderPartRequest) (*inventoryv1.ReturnRepairOrderPartResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.ReturnRepairOrderPartResponse, error) {
		in := req.GetPartReturn()

		r, err := s.uc.ReturnRepairOrderPart(ctx, usecase.ReturnRepairOrderPartRequest{
			GarageID: req.GetGarageId(), RepairOrderID: req.GetRepairOrderId(), ActorID: actorID,
			RepairOrderLineID: in.GetRepairOrderLineId(), UnitID: in.GetUnitId(), EnteredQuantity: in.GetEnteredQuantity(),
			Reason: in.GetReason(),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.ReturnRepairOrderPartResponse{PartReturn: partReturnToProto(ctx, r)}, nil
	})
}

// ListRepairOrderPartReturns lists the parts returned from a repair order.
func (s *Server) ListRepairOrderPartReturns(ctx context.Context, req *inventoryv1.ListRepairOrderPartReturnsRequest) (*inventoryv1.ListRepairOrderPartReturnsResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListRepairOrderPartReturnsResponse, error) {
		items, err := s.uc.ListRepairOrderPartReturns(ctx, req.GetGarageId(), req.GetRepairOrderId())
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.ListRepairOrderPartReturnsResponse{Data: make([]*inventoryv1.RepairOrderPartReturn, 0, len(items))}
		for i := range items {
			resp.Data = append(resp.Data, partReturnToProto(ctx, &items[i]))
		}

		return resp, nil
	})
}

func writeOffLinesFromProto(lines []*inventoryv1.WriteOffLine) []usecase.AdjustmentLineInput {
	out := make([]usecase.AdjustmentLineInput, 0, len(lines))

	for _, l := range lines {
		out = append(out, usecase.AdjustmentLineInput{PartID: l.GetPartId(), UnitID: l.GetUnitId(), EnteredQuantity: l.GetEnteredQuantity()})
	}

	return out
}

func supplierReturnLinesFromProto(lines []*inventoryv1.SupplierReturnLine) []usecase.AdjustmentLineInput {
	out := make([]usecase.AdjustmentLineInput, 0, len(lines))

	for _, l := range lines {
		out = append(out, usecase.AdjustmentLineInput{
			PartID: l.GetPartId(), UnitID: l.GetUnitId(), EnteredQuantity: l.GetEnteredQuantity(), UnitCost: l.GetUnitCost(),
		})
	}

	return out
}

func writeOffToProto(ctx context.Context, w *domain.WriteOff) *inventoryv1.WriteOff {
	seeCost := canSeeCost(ctx)

	out := &inventoryv1.WriteOff{
		Id: w.ID, GarageId: w.GarageID, Number: w.Number, Status: adjustmentStatusToProto(w.Status),
		Reason: writeOffReasonToProto(w.Reason), Notes: w.Notes, Lines: make([]*inventoryv1.WriteOffLine, 0, len(w.Lines)),
		CancelledAt: converter.TimePtrToProto(w.CancelledAt), CreatedByName: w.CreatedByName,
		CreatedAt: converter.TimeToProto(w.CreatedAt), UpdatedAt: converter.TimeToProto(w.UpdatedAt),
	}

	if seeCost {
		out.TotalValue = &w.TotalValue
	}

	for i := range w.Lines {
		l := &w.Lines[i]
		line := &inventoryv1.WriteOffLine{
			Id: l.ID, PartId: l.PartID, PartSku: l.PartSKU, PartName: l.PartName, BaseUnitAbbreviation: l.BaseUnitAbbreviation,
			UnitId: l.UnitID, UnitAbbreviation: l.UnitAbbreviation, EnteredQuantity: l.EnteredQuantity, Quantity: l.Quantity,
		}

		if seeCost {
			line.UnitCost, line.LineValue = wholeDongRef(l.AverageCost), new(l.Value())
		}

		out.Lines = append(out.Lines, line)
	}

	return out
}

func supplierReturnToProto(ctx context.Context, r *domain.SupplierReturn) *inventoryv1.SupplierReturn {
	seeCost := canSeeCost(ctx)

	out := &inventoryv1.SupplierReturn{
		Id: r.ID, GarageId: r.GarageID, Number: r.Number, Status: adjustmentStatusToProto(r.Status), SupplierId: r.SupplierID,
		SupplierName: r.SupplierName, GoodsReceiptId: r.GoodsReceiptID, GoodsReceiptNumber: r.GoodsReceiptNumber,
		CreditNoteNumber: r.CreditNoteNumber, Notes: r.Notes, Lines: make([]*inventoryv1.SupplierReturnLine, 0, len(r.Lines)),
		CancelledAt: converter.TimePtrToProto(r.CancelledAt), CreatedByName: r.CreatedByName,
		CreatedAt: converter.TimeToProto(r.CreatedAt), UpdatedAt: converter.TimeToProto(r.UpdatedAt),
	}

	if seeCost {
		out.CreditAmount = &r.CreditAmount
	}

	for i := range r.Lines {
		l := &r.Lines[i]
		line := &inventoryv1.SupplierReturnLine{
			Id: l.ID, PartId: l.PartID, PartSku: l.PartSKU, PartName: l.PartName, BaseUnitAbbreviation: l.BaseUnitAbbreviation,
			UnitId: l.UnitID, UnitAbbreviation: l.UnitAbbreviation, EnteredQuantity: l.EnteredQuantity, Quantity: l.Quantity,
		}

		if seeCost {
			line.UnitCost, line.LineAmount = &l.CreditUnitCost, new(l.Amount())
		}

		out.Lines = append(out.Lines, line)
	}

	return out
}

func partReturnToProto(ctx context.Context, r *domain.RepairOrderPartReturn) *inventoryv1.RepairOrderPartReturn {
	out := &inventoryv1.RepairOrderPartReturn{
		Id: r.ID, GarageId: r.GarageID, RepairOrderId: r.RepairOrderID, RepairOrderNumber: r.RepairOrderNumber,
		StockIssueId: r.StockIssueID, RepairOrderLineId: r.RepairOrderLineID, PartId: r.PartID, PartSku: r.PartSKU,
		PartName: r.PartName, BaseUnitAbbreviation: r.BaseUnitAbbreviation, UnitId: r.UnitID, UnitAbbreviation: r.UnitAbbreviation,
		EnteredQuantity: r.EnteredQuantity, Quantity: r.Quantity, Reason: r.Reason, CreatedByName: r.CreatedByName,
		CreatedAt: converter.TimeToProto(r.CreatedAt),
	}

	if canSeeCost(ctx) {
		out.UnitCost = wholeDongRef(r.UnitCost)
	}

	return out
}

func adjustmentStatusToProto(s domain.AdjustmentStatus) inventoryv1.AdjustmentStatus {
	switch s {
	case domain.AdjustmentCompleted:
		return inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_COMPLETED
	case domain.AdjustmentCancelled:
		return inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_CANCELLED
	default:
		return inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_UNSPECIFIED
	}
}

// adjustmentStatusFromProto maps UNSPECIFIED (and unknown values) to "", which
// the use case treats as "every status" when listing.
func adjustmentStatusFromProto(s inventoryv1.AdjustmentStatus) domain.AdjustmentStatus {
	switch s {
	case inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_COMPLETED:
		return domain.AdjustmentCompleted
	case inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_CANCELLED:
		return domain.AdjustmentCancelled
	case inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

var writeOffReasons = []struct {
	domain domain.WriteOffReason
	proto  inventoryv1.WriteOffReason
}{
	{domain.WriteOffDamaged, inventoryv1.WriteOffReason_WRITE_OFF_REASON_DAMAGED},
	{domain.WriteOffLost, inventoryv1.WriteOffReason_WRITE_OFF_REASON_LOST},
	{domain.WriteOffExpired, inventoryv1.WriteOffReason_WRITE_OFF_REASON_EXPIRED},
	{domain.WriteOffInternalUse, inventoryv1.WriteOffReason_WRITE_OFF_REASON_INTERNAL_USE},
	{domain.WriteOffWarrantyReplacement, inventoryv1.WriteOffReason_WRITE_OFF_REASON_WARRANTY_REPLACEMENT},
}

func writeOffReasonToProto(r domain.WriteOffReason) inventoryv1.WriteOffReason {
	for _, m := range writeOffReasons {
		if m.domain == r {
			return m.proto
		}
	}

	return inventoryv1.WriteOffReason_WRITE_OFF_REASON_UNSPECIFIED
}

// writeOffReasonFromProto maps UNSPECIFIED (and unknown values) to "", which
// the use case rejects when creating and treats as "every reason" when listing.
func writeOffReasonFromProto(r inventoryv1.WriteOffReason) domain.WriteOffReason {
	for _, m := range writeOffReasons {
		if m.proto == r {
			return m.domain
		}
	}

	return ""
}
