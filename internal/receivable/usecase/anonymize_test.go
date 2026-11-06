package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
)

func TestReceivableUseCase_HandleCustomerAnonymized(t *testing.T) {
	t.Parallel()

	t.Run("replaces the name and plate copied into the customer's receivables", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().AnonymizeCustomer(mock.Anything, garageID, customerID).Return(nil).Once()

		require.NoError(t, uc.HandleCustomerAnonymized(t.Context(), event(t, garageID, map[string]string{"customer_id": customerID})))
	})

	t.Run("a failure is returned so the event is delivered again", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().AnonymizeCustomer(mock.Anything, garageID, customerID).Return(errBoom).Once()

		require.ErrorIs(t, uc.HandleCustomerAnonymized(t.Context(), event(t, garageID, map[string]string{"customer_id": customerID})), errBoom)
	})

	t.Run("a malformed event is dropped", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		require.NoError(t, uc.HandleCustomerAnonymized(t.Context(), event(t, garageID, "not an object")))
		require.NoError(t, uc.HandleCustomerAnonymized(t.Context(), event(t, garageID, map[string]string{})))
		require.NoError(t, uc.HandleCustomerAnonymized(t.Context(), event(t, "", map[string]string{"customer_id": customerID})))
	})
}

func TestReceivableUseCase_SyncFollowsTheLiveCustomer(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.lock()

	view := settlementView()
	view.CustomerAnonymized = true

	existing := openReceivable(day(2026, 11, 17))
	existing.CustomerName, existing.VehicleLicensePlate = "", ""

	d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(view, nil).Once()
	d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(existing, nil).Once()
	d.Repo.EXPECT().Save(mock.Anything, existing).Return(nil).Once()
	d.Repo.EXPECT().AddEntries(mock.Anything, garageID, receivableID, mock.Anything).Return(nil).Once()

	require.NoError(t, uc.Sync(t.Context(), garageID, settlementID))
	require.Equal(t, domain.AnonymizedName, existing.CustomerName, "a receivable made after the customer was anonymized never holds the real name")
	require.Equal(t, domain.AnonymizedPlate, existing.VehicleLicensePlate)
}

func TestReceivableUseCase_SyncKeepsAnAnonymizedName(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.lock()

	existing := openReceivable(day(2026, 11, 17))
	existing.CustomerName, existing.VehicleLicensePlate = domain.AnonymizedName, domain.AnonymizedPlate

	d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(settlementView(), nil).Once()
	d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(existing, nil).Once()
	d.Repo.EXPECT().Save(mock.Anything, existing).Return(nil).Once()
	d.Repo.EXPECT().AddEntries(mock.Anything, garageID, receivableID, mock.Anything).Return(nil).Once()

	require.NoError(t, uc.Sync(t.Context(), garageID, settlementID))
	require.Equal(t, domain.AnonymizedName, existing.CustomerName, "the settlement still holds the old name; the receivable does not take it back")
	require.Equal(t, domain.AnonymizedPlate, existing.VehicleLicensePlate)
	require.True(t, existing.Anonymized())
}
