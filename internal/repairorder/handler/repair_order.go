package handler

import (
	"context"
	"slices"

	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"

	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
)

// ListRepairOrders lists a garage's repair orders (the Kanban board's
// data), optionally filtered by vehicle, status or assigned technician.
func (s *Server) ListRepairOrders(
	ctx context.Context, req *repairorderv1.ListRepairOrdersRequest,
) (*repairorderv1.ListRepairOrdersResponse, error) {
	return run(ctx, func(string) (*repairorderv1.ListRepairOrdersResponse, error) {
		page, err := s.uc.List(ctx, usecase.ListRepairOrdersRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(),
			VehicleID: req.GetVehicleId(), Status: statusFromProto(req.GetStatus()), TechnicianID: req.GetTechnicianId(),
		})
		if err != nil {
			return nil, err
		}

		resp := &repairorderv1.ListRepairOrdersResponse{
			Data: make([]*repairorderv1.RepairOrder, 0, len(page.Items)), Pagination: paginationToProto(page),
		}
		for _, ro := range page.Items {
			resp.Data = append(resp.Data, repairOrderToProto(ro))
		}

		return resp, nil
	})
}

// CreateRepairOrder creates an ad-hoc repair order (FR-REP-01); answers 201.
func (s *Server) CreateRepairOrder(
	ctx context.Context, req *repairorderv1.CreateRepairOrderRequest,
) (*repairorderv1.CreateRepairOrderResponse, error) {
	return run(ctx, func(actorID string) (*repairorderv1.CreateRepairOrderResponse, error) {
		ro, err := s.uc.Create(ctx, usecase.CreateRepairOrderRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, VehicleID: req.GetRepairOrder().GetVehicleId(),
			Lines: lineInputsFromProto(req.GetRepairOrder().GetLines()),
		})
		if err != nil {
			return nil, err
		}

		return &repairorderv1.CreateRepairOrderResponse{RepairOrder: repairOrderToProto(ro)}, nil
	})
}

// GetRepairOrder returns one repair order with its lines.
func (s *Server) GetRepairOrder(
	ctx context.Context, req *repairorderv1.GetRepairOrderRequest,
) (*repairorderv1.GetRepairOrderResponse, error) {
	return run(ctx, func(string) (*repairorderv1.GetRepairOrderResponse, error) {
		ro, err := s.uc.Get(ctx, req.GetGarageId(), req.GetRepairOrderId())
		if err != nil {
			return nil, err
		}

		return &repairorderv1.GetRepairOrderResponse{RepairOrder: repairOrderToProto(ro)}, nil
	})
}

var lineSettable = []string{
	"status", "assigned_technician_ids", "description", "item_ref_id", "quantity", "unit_id", "entered_quantity", "unit_price",
}

// lineEdit reads the content fields named by paths from line; nil when none is.
func lineEdit(paths []string, line *repairorderv1.RepairOrderLine) *usecase.LineEdit {
	var edit usecase.LineEdit

	touched := false

	for _, path := range paths {
		switch path {
		case "description":
			edit.Description = new(line.GetDescription())
		case "item_ref_id":
			edit.ItemRefID = new(line.GetItemRefId())
		case "quantity":
			edit.Quantity = new(line.GetQuantity())
		case "unit_id":
			edit.UnitID = new(line.GetUnitId())
		case "entered_quantity":
			edit.EnteredQuantity = new(line.GetEnteredQuantity())
		case "unit_price":
			edit.UnitPrice = new(line.GetUnitPrice())
		default:
			continue
		}

		touched = true
	}

	if !touched {
		return nil
	}

	return &edit
}

// UpdateRepairOrderLine assigns technicians and/or updates one line's own
// progress (FR-REP-02/03).
func (s *Server) UpdateRepairOrderLine(
	ctx context.Context, req *repairorderv1.UpdateRepairOrderLineRequest,
) (*repairorderv1.UpdateRepairOrderLineResponse, error) {
	return run(ctx, func(actorID string) (*repairorderv1.UpdateRepairOrderLineResponse, error) {
		paths := maskPaths(req.GetUpdateMask().GetPaths(), "status", "assigned_technician_ids")
		for _, path := range paths {
			if !slices.Contains(lineSettable, path) {
				return nil, badMask(path)
			}
		}

		update := usecase.UpdateLineRequest{
			GarageID: req.GetGarageId(), RepairOrderID: req.GetRepairOrderId(), LineID: req.GetLineId(), ActorID: actorID,
		}

		if slices.Contains(paths, "status") {
			status := lineStatusFromProto(req.GetLine().GetStatus())
			update.Status = &status
		}

		if slices.Contains(paths, "assigned_technician_ids") {
			ids := req.GetLine().GetAssignedTechnicianIds()
			update.AssignedTechnicianIDs = &ids
		}

		update.Edit = lineEdit(paths, req.GetLine())

		ro, err := s.uc.UpdateLine(ctx, update)
		if err != nil {
			return nil, err
		}

		return &repairorderv1.UpdateRepairOrderLineResponse{RepairOrder: repairOrderToProto(ro)}, nil
	})
}

// CloseRepairOrder closes the repair order (FR-REP-06).
func (s *Server) CloseRepairOrder(
	ctx context.Context, req *repairorderv1.CloseRepairOrderRequest,
) (*repairorderv1.CloseRepairOrderResponse, error) {
	return run(ctx, func(actorID string) (*repairorderv1.CloseRepairOrderResponse, error) {
		ro, err := s.uc.Close(ctx, req.GetGarageId(), req.GetRepairOrderId(), actorID)
		if err != nil {
			return nil, err
		}

		return &repairorderv1.CloseRepairOrderResponse{RepairOrder: repairOrderToProto(ro)}, nil
	})
}

// AddRepairOrderLine adds a line to a repair order (Release 7.3).
func (s *Server) AddRepairOrderLine(
	ctx context.Context, req *repairorderv1.AddRepairOrderLineRequest,
) (*repairorderv1.AddRepairOrderLineResponse, error) {
	return run(ctx, func(actorID string) (*repairorderv1.AddRepairOrderLineResponse, error) {
		ro, err := s.uc.AddLine(ctx, usecase.AddLineRequest{
			GarageID: req.GetGarageId(), RepairOrderID: req.GetRepairOrderId(), ActorID: actorID,
			Line: lineInputFromProto(req.GetLine()),
		})
		if err != nil {
			return nil, err
		}

		return &repairorderv1.AddRepairOrderLineResponse{RepairOrder: repairOrderToProto(ro)}, nil
	})
}

// DeleteRepairOrderLine removes a line from a repair order (Release 7.3).
func (s *Server) DeleteRepairOrderLine(
	ctx context.Context, req *repairorderv1.DeleteRepairOrderLineRequest,
) (*repairorderv1.DeleteRepairOrderLineResponse, error) {
	return run(ctx, func(actorID string) (*repairorderv1.DeleteRepairOrderLineResponse, error) {
		ro, err := s.uc.DeleteLine(ctx, req.GetGarageId(), req.GetRepairOrderId(), req.GetLineId(), actorID)
		if err != nil {
			return nil, err
		}

		return &repairorderv1.DeleteRepairOrderLineResponse{RepairOrder: repairOrderToProto(ro)}, nil
	})
}

// CancelRepairOrder cancels a repair order whose work has not started
// (Release 7.3).
func (s *Server) CancelRepairOrder(
	ctx context.Context, req *repairorderv1.CancelRepairOrderRequest,
) (*repairorderv1.CancelRepairOrderResponse, error) {
	return run(ctx, func(actorID string) (*repairorderv1.CancelRepairOrderResponse, error) {
		ro, err := s.uc.Cancel(ctx, req.GetGarageId(), req.GetRepairOrderId(), actorID)
		if err != nil {
			return nil, err
		}

		return &repairorderv1.CancelRepairOrderResponse{RepairOrder: repairOrderToProto(ro)}, nil
	})
}
