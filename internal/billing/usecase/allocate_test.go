package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestSettlementUseCase_Allocate(t *testing.T) {
	t.Parallel()

	t.Run("splits the settlement between customer and insurer", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Partners.EXPECT().FindInsurer(mock.Anything, garageID, insurerID).Return("ABC Insurance", nil).Once()
		d.Settlements.EXPECT().SaveAllocation(mock.Anything, mock.Anything).Return(nil).Once()

		s, err := uc.Allocate(t.Context(), usecase.AllocateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, ActorID: actorID,
			CustomerAmount: 600_000, InsurerAmount: 400_000, InsurerPartnerID: insurerIDPtr(),
		})
		require.NoError(t, err)
		require.Equal(t, int64(600_000), s.CustomerAmount)
		require.Equal(t, int64(400_000), s.InsurerAmount)
		require.Equal(t, "ABC Insurance", s.InsurerPartnerName)
		require.Equal(t, insurerID, *s.InsurerPartnerID)
	})

	t.Run("fully allocated to customer needs no insurer lookup", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Settlements.EXPECT().SaveAllocation(mock.Anything, mock.Anything).Return(nil).Once()

		s, err := uc.Allocate(t.Context(), usecase.AllocateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, CustomerAmount: 1_000_000, InsurerAmount: 0,
		})
		require.NoError(t, err)
		require.Equal(t, int64(1_000_000), s.CustomerAmount)
	})

	t.Run("settlement not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrSettlementNotFound).Once()

		_, err := uc.Allocate(t.Context(), usecase.AllocateSettlementRequest{GarageID: garageID, SettlementID: "missing"})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("split does not sum to total", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

		_, err := uc.Allocate(t.Context(), usecase.AllocateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, CustomerAmount: 600_000, InsurerAmount: 300_000,
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("insurer amount without a partner", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

		_, err := uc.Allocate(t.Context(), usecase.AllocateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, CustomerAmount: 600_000, InsurerAmount: 400_000,
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("insurer partner not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Partners.EXPECT().FindInsurer(mock.Anything, garageID, insurerID).Return("", domain.ErrInsurerNotFound).Once()

		_, err := uc.Allocate(t.Context(), usecase.AllocateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, CustomerAmount: 600_000, InsurerAmount: 400_000,
			InsurerPartnerID: insurerIDPtr(),
		})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("new allocation would be below what was already paid", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		s := openSettlement()
		s.CustomerAmount, s.CustomerPaidAmount = 1_000_000, 800_000
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(s, nil).Once()
		d.Partners.EXPECT().FindInsurer(mock.Anything, garageID, insurerID).Return("ABC Insurance", nil).Once()

		_, err := uc.Allocate(t.Context(), usecase.AllocateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, CustomerAmount: 600_000, InsurerAmount: 400_000,
			InsurerPartnerID: insurerIDPtr(),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Settlements.EXPECT().SaveAllocation(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.Allocate(t.Context(), usecase.AllocateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, CustomerAmount: 1_000_000, InsurerAmount: 0,
		})
		assertAppError(t, err, apperror.CodeInternal)
	})
}
