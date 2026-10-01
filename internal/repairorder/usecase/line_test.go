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
