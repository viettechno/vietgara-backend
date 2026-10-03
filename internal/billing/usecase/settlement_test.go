package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestSettlementUseCase_Create(t *testing.T) {
	t.Parallel()

	t.Run("creates a settlement from a closed repair order", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).
			Return(nil, domain.ErrSettlementNotFound).Once()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(closedRepairOrder(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "settlement", "QT-202609-{SEQ:4}").Return(1, nil).Once()
		d.Settlements.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventSettlementCreated, garageID, "", mock.Anything).Return(nil).Once()

		s, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{
			GarageID: garageID, ActorID: actorID, RepairOrderID: repairOrderID,
		})
		require.NoError(t, err)
		require.Equal(t, "An Nguyen", s.CustomerName)
		require.Equal(t, "51F-123.45", s.VehicleLicensePlate)
		require.Equal(t, "QT-202609-0001", s.Number)
		require.Equal(t, int64(1_000_000), s.TotalAmount)
		require.Equal(t, int64(1_000_000), s.CustomerAmount)
		require.Equal(t, domain.StatusPending, s.Status)
	})

	t.Run("already settled", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).
			Return(openSettlement(), nil).Once()

		_, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{GarageID: garageID, RepairOrderID: repairOrderID})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})

	t.Run("lookup failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).Return(nil, errBoom).Once()

		_, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{GarageID: garageID, RepairOrderID: repairOrderID})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("repair order not closed", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).
			Return(nil, domain.ErrSettlementNotFound).Once()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).
			Return(usecase.RepairOrderInfo{Closed: false}, nil).Once()

		_, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{GarageID: garageID, RepairOrderID: repairOrderID})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("plan limit reached", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).
			Return(nil, domain.ErrSettlementNotFound).Once()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(closedRepairOrder(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()

		planLimit := apperror.New(apperror.CodeFailedPrecondition, "limit reached")
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(planLimit).Once()

		_, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{GarageID: garageID, RepairOrderID: repairOrderID})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).
			Return(nil, domain.ErrSettlementNotFound).Once()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(closedRepairOrder(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "settlement", "QT-202609-{SEQ:4}").Return(1, nil).Once()
		d.Settlements.EXPECT().Create(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{GarageID: garageID, RepairOrderID: repairOrderID})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("event publish failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).
			Return(nil, domain.ErrSettlementNotFound).Once()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(closedRepairOrder(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "settlement", "QT-202609-{SEQ:4}").Return(1, nil).Once()
		d.Settlements.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventSettlementCreated, garageID, "", mock.Anything).Return(errBoom).Once()

		_, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{GarageID: garageID, RepairOrderID: repairOrderID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestSettlementUseCase_Get(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

	s, err := uc.Get(t.Context(), garageID, settlementID)
	require.NoError(t, err)
	require.Equal(t, settlementID, s.ID)

	d.Settlements.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrSettlementNotFound).Once()

	_, err = uc.Get(t.Context(), garageID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestSettlementUseCase_List(t *testing.T) {
	t.Parallel()

	t.Run("by status", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		want := []*domain.Settlement{openSettlement()}
		d.Settlements.EXPECT().
			List(mock.Anything, garageID, usecase.ListFilter{Status: domain.StatusPending, Limit: 20, Offset: 0}).
			Return(want, 1, nil).Once()

		page, err := uc.List(t.Context(), usecase.ListSettlementsRequest{GarageID: garageID, Status: domain.StatusPending})
		require.NoError(t, err)
		require.Equal(t, 1, page.Total)
		require.Equal(t, want, page.Items)
	})

	t.Run("outstanding only, FR-REC-01 receivables view", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().
			List(mock.Anything, garageID, usecase.ListFilter{OutstandingOnly: true, Limit: 20, Offset: 0}).
			Return(nil, 0, nil).Once()

		_, err := uc.List(t.Context(), usecase.ListSettlementsRequest{GarageID: garageID, OutstandingOnly: true})
		require.NoError(t, err)
	})

	t.Run("status takes precedence over outstanding_only", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().
			List(mock.Anything, garageID, usecase.ListFilter{Status: domain.StatusPaid, Limit: 20, Offset: 0}).
			Return(nil, 0, nil).Once()

		_, err := uc.List(t.Context(), usecase.ListSettlementsRequest{
			GarageID: garageID, Status: domain.StatusPaid, OutstandingOnly: true,
		})
		require.NoError(t, err)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err := uc.List(t.Context(), usecase.ListSettlementsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}
