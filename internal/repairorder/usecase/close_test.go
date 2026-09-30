package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func completedRepairOrder() *domain.RepairOrder {
	ro := openRepairOrder()
	itemRef := "item-1"
	ro.Lines = append(ro.Lines, domain.Line{
		ID: "line-2", Position: 2, ItemType: domain.ItemTypePart, ItemRefID: &itemRef, Description: "Air filter",
		Quantity: "1", UnitPrice: 480_000, LineTotal: 480_000, Status: domain.LineCompleted,
	})
	ro.Lines[0].Status = domain.LineCompleted
	ro.Status = domain.StatusCompleted

	return ro
}

func TestRepairOrderUseCase_Close(t *testing.T) {
	t.Parallel()

	t.Run("closes and publishes repair_order.closed with the part lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(completedRepairOrder(), nil).Once()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().
			Publish(mock.Anything, usecase.EventRepairOrderClosed, garageID, "", mock.MatchedBy(func(payload map[string]any) bool {
				return payload["repair_order_id"] == repairOrderID && payload["part_lines"] != nil
			})).
			Return(nil).Once()

		ro, err := uc.Close(t.Context(), garageID, repairOrderID, actorID)
		require.NoError(t, err)
		require.Equal(t, domain.StatusClosed, ro.Status)
		require.NotNil(t, ro.ClosedAt)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrRepairOrderNotFound).Once()

		_, err := uc.Close(t.Context(), garageID, "missing", actorID)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("already closed", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := completedRepairOrder()
		ro.Status = domain.StatusClosed
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(ro, nil).Once()

		_, err := uc.Close(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("not ready to close", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()

		_, err := uc.Close(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("save failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(completedRepairOrder(), nil).Once()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.Close(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("publish failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(completedRepairOrder(), nil).Once()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderClosed, garageID, "", mock.Anything).Return(errBoom).Once()

		_, err := uc.Close(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}
