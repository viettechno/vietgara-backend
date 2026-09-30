package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

// UpdateLineRequest is the input for RepairOrderUseCase.UpdateLine; nil
// fields keep their current value.
type UpdateLineRequest struct {
	GarageID              string
	RepairOrderID         string
	LineID                string
	ActorID               string
	Status                *domain.LineStatus
	AssignedTechnicianIDs *[]string
}

// UpdateLine assigns technicians and/or updates one line's own progress
// (FR-REP-02/03). The repair order's overall status (the Kanban column) is
// derived from its lines afterwards.
func (uc *RepairOrderUseCase) UpdateLine(ctx context.Context, req UpdateLineRequest) (*domain.RepairOrder, error) {
	ro, err := uc.RepairOrders.FindByID(ctx, req.GarageID, req.RepairOrderID)
	if err != nil {
		return nil, toAppError("update repair order line", err)
	}

	if ro.Status == domain.StatusClosed {
		return nil, toAppError("update repair order line", domain.ErrAlreadyClosed)
	}

	index := -1

	for i := range ro.Lines {
		if ro.Lines[i].ID == req.LineID {
			index = i

			break
		}
	}

	if index < 0 {
		return nil, toAppError("update repair order line", domain.ErrLineNotFound)
	}

	if req.Status != nil {
		if !req.Status.IsValid() {
			return nil, toAppError("update repair order line", domain.ErrInvalidLineStatus)
		}

		ro.Lines[index].Status = *req.Status
	}

	if req.AssignedTechnicianIDs != nil {
		ro.Lines[index].AssignedTechnicianIDs = *req.AssignedTechnicianIDs
	}

	ro.DeriveStatus()
	ro.UpdatedBy = actorRef(req.ActorID)

	if err := uc.RepairOrders.Save(ctx, ro); err != nil {
		return nil, toAppError("update repair order line", err)
	}

	return ro, nil
}
