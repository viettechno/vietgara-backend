package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestRepairOrderUseCase_UpdateLine(t *testing.T) {
	t.Parallel()

	t.Run("updates a line's status and assigned technicians", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := openRepairOrder()
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(ro, nil).Once()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		started := d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderStarted, garageID, "", mock.Anything).Return(nil).Once()
		// In the order it happened: the order started, then it completed.
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderCompleted, garageID, "", mock.MatchedBy(func(p map[string]any) bool {
			return p["repair_order_id"] == repairOrderID && p["vehicle_id"] != nil && p["number"] != nil
		})).NotBefore(started).Return(nil).Once()

		status := domain.LineCompleted
		ids := []string{actorID}

		updated, err := uc.UpdateLine(t.Context(), usecase.UpdateLineRequest{
			GarageID: garageID, RepairOrderID: repairOrderID, LineID: "line-1", ActorID: actorID,
			Status: &status, AssignedTechnicianIDs: &ids,
		})
		require.NoError(t, err)
		require.Equal(t, domain.LineCompleted, updated.Lines[0].Status)
		require.Equal(t, []string{actorID}, updated.Lines[0].AssignedTechnicianIDs)
		require.Equal(t, domain.StatusCompleted, updated.Status)
	})

	t.Run("repair order not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrRepairOrderNotFound).Once()

		_, err := uc.UpdateLine(t.Context(), usecase.UpdateLineRequest{GarageID: garageID, RepairOrderID: "missing"})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("already closed", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := openRepairOrder()
		ro.Status = domain.StatusClosed
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(ro, nil).Once()

		_, err := uc.UpdateLine(t.Context(), usecase.UpdateLineRequest{GarageID: garageID, RepairOrderID: repairOrderID})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("line not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()

		_, err := uc.UpdateLine(t.Context(), usecase.UpdateLineRequest{
			GarageID: garageID, RepairOrderID: repairOrderID, LineID: "missing",
		})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("invalid status", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()

		bad := domain.LineStatus("bogus")

		_, err := uc.UpdateLine(t.Context(), usecase.UpdateLineRequest{
			GarageID: garageID, RepairOrderID: repairOrderID, LineID: "line-1", Status: &bad,
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("save failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(errBoom).Once()

		status := domain.LineInProgress

		_, err := uc.UpdateLine(t.Context(), usecase.UpdateLineRequest{
			GarageID: garageID, RepairOrderID: repairOrderID, LineID: "line-1", Status: &status,
		})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestRepairOrderUseCase_UpdateLine_StartedEvent(t *testing.T) {
	t.Parallel()

	update := func(uc *usecase.RepairOrderUseCase, status domain.LineStatus) error {
		_, err := uc.UpdateLine(t.Context(), usecase.UpdateLineRequest{
			GarageID: garageID, RepairOrderID: repairOrderID, LineID: "line-1", ActorID: actorID, Status: &status,
		})

		return err
	}

	t.Run("is published once, with the part lines, when the order leaves Open", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := openRepairOrder()
		part := "part-1"
		ro.Lines = append(ro.Lines, domain.Line{
			ID: "line-2", Position: 2, ItemType: domain.ItemTypePart, ItemRefID: &part, Quantity: "48", UnitID: new("unit-1"),
			EnteredQuantity: new("2"), Status: domain.LineNotStarted,
		})

		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(ro, nil).Twice()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Twice()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderStarted, garageID, "", mock.MatchedBy(func(p map[string]any) bool { return p["repair_order_id"] == repairOrderID })).Return(nil).Once()

		require.NoError(t, update(uc, domain.LineInProgress))
		// The order is already in progress now: no second event.
		require.NoError(t, update(uc, domain.LineInProgress))
	})

	t.Run("a failure to publish fails the update", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderStarted, garageID, "", mock.Anything).Return(errBoom).Once()

		assertAppError(t, update(uc, domain.LineInProgress), apperror.CodeInternal)
	})

	t.Run("an update that leaves the order Open publishes nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()

		require.NoError(t, update(uc, domain.LineNotStarted))
	})
}

func TestRepairOrderUseCase_UpdateLineCompletedOnce(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	ro := openRepairOrder()
	ro.Status = domain.StatusCompleted
	ro.Lines[0].Status = domain.LineCompleted
	d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(ro, nil).Once()
	d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()

	ids := []string{actorID}

	// The order was already completed, so changing an assignment announces nothing.
	_, err := uc.UpdateLine(t.Context(), usecase.UpdateLineRequest{
		GarageID: garageID, RepairOrderID: repairOrderID, LineID: "line-1", ActorID: actorID, AssignedTechnicianIDs: &ids,
	})
	require.NoError(t, err)
}
