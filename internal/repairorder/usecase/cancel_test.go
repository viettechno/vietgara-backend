package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestRepairOrderUseCase_Cancel(t *testing.T) {
	t.Parallel()

	for _, status := range []domain.RepairOrderStatus{domain.StatusOpen, domain.StatusWaitingOnParts} {
		t.Run("cancels an order that is "+string(status), func(t *testing.T) {
			t.Parallel()

			uc, d := newUseCase(t)
			ro := openRepairOrder()
			ro.Status, ro.WaitingOnParts = status, status == domain.StatusWaitingOnParts
			expectFind(d, ro)
			d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
			d.RepairOrders.EXPECT().Save(mock.Anything, mock.MatchedBy(func(r *domain.RepairOrder) bool {
				return r.Status == domain.StatusCancelled && !r.WaitingOnParts
			})).Return(nil).Once()
			d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderCancelled, garageID, "", mock.Anything).Return(nil).Once()

			got, err := uc.Cancel(t.Context(), garageID, repairOrderID, actorID)
			require.NoError(t, err)
			assert.Equal(t, domain.StatusCancelled, got.Status)
		})
	}

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()

		started := openRepairOrder()
		started.Lines[0].Status, started.Status = domain.LineInProgress, domain.StatusInProgress

		waitingButStarted := openRepairOrder()
		waitingButStarted.Lines[0].Status = domain.LineInProgress
		waitingButStarted.Status = domain.StatusWaitingOnParts

		completed, closed, cancelled := openRepairOrder(), openRepairOrder(), openRepairOrder()
		completed.Status, closed.Status, cancelled.Status = domain.StatusCompleted, domain.StatusClosed, domain.StatusCancelled

		for name, ro := range map[string]*domain.RepairOrder{
			"in progress": started, "waiting but started": waitingButStarted, "completed": completed, "closed": closed,
			"already cancelled": cancelled,
		} {
			uc, d := newUseCase(t)
			expectFind(d, ro)

			_, err := uc.Cancel(t.Context(), garageID, repairOrderID, actorID)
			require.Error(t, err, name)
			assertAppError(t, err, apperror.CodeFailedPrecondition)
		}
	})

	t.Run("an order whose stock issue is posted cannot be cancelled", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(true, nil).Once()

		_, err := uc.Cancel(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(nil, domain.ErrRepairOrderNotFound).Once()
		_, err := uc.Cancel(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeNotFound)

		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, errBoom).Once()
		_, err = uc.Cancel(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.Cancel(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderCancelled, garageID, "", mock.Anything).Return(errBoom).Once()
		_, err = uc.Cancel(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("a cancelled order cannot be closed", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := openRepairOrder()
		ro.Status = domain.StatusCancelled
		expectFind(d, ro)

		_, err := uc.Close(t.Context(), garageID, repairOrderID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})
}

func shortageEvent(short string) outbox.Event {
	return outbox.Event{GarageID: garageID, Data: []byte(`{"repair_order_id":"` + repairOrderID + `","short":` + short + `}`)}
}

func TestRepairOrderUseCase_HandleStockShortage(t *testing.T) {
	t.Parallel()

	t.Run("a shortage makes the order wait on parts", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, openRepairOrder())
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.MatchedBy(func(r *domain.RepairOrder) bool {
			return r.WaitingOnParts && r.Status == domain.StatusWaitingOnParts
		})).Return(nil).Once()

		require.NoError(t, uc.HandleStockShortage(t.Context(), shortageEvent("true")))
	})

	t.Run("stock that arrives brings the order back", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := openRepairOrder()
		ro.WaitingOnParts, ro.Status = true, domain.StatusWaitingOnParts
		expectFind(d, ro)
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.MatchedBy(func(r *domain.RepairOrder) bool {
			return !r.WaitingOnParts && r.Status == domain.StatusOpen
		})).Return(nil).Once()

		require.NoError(t, uc.HandleStockShortage(t.Context(), shortageEvent("false")))
	})

	t.Run("a redelivered event, or a final order, is left alone", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		same := openRepairOrder()
		same.WaitingOnParts = true
		closed, cancelled := openRepairOrder(), openRepairOrder()
		closed.Status, cancelled.Status = domain.StatusClosed, domain.StatusCancelled

		for _, ro := range []*domain.RepairOrder{same, closed, cancelled} {
			expectFind(d, ro)
			require.NoError(t, uc.HandleStockShortage(t.Context(), shortageEvent("true")))
		}
	})

	t.Run("an unknown order or a malformed event is dropped", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(nil, domain.ErrRepairOrderNotFound).Once()

		require.NoError(t, uc.HandleStockShortage(t.Context(), shortageEvent("true")))
		require.NoError(t, uc.HandleStockShortage(t.Context(), outbox.Event{GarageID: garageID, Data: []byte(`nope`)}))
		require.NoError(t, uc.HandleStockShortage(t.Context(), outbox.Event{GarageID: garageID, Data: []byte(`{}`)}))
	})

	t.Run("failures are returned for a retry", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(nil, errBoom).Once()
		require.Error(t, uc.HandleStockShortage(t.Context(), shortageEvent("true")))

		expectFind(d, openRepairOrder())
		d.RepairOrders.EXPECT().Save(mock.Anything, mock.Anything).Return(errBoom).Once()
		require.Error(t, uc.HandleStockShortage(t.Context(), shortageEvent("true")))
	})
}
