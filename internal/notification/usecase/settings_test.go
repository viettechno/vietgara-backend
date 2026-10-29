package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
)

func TestUpdateSettings(t *testing.T) {
	t.Parallel()

	t.Run("saves the types that are off and trims the sender name", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.Settings.EXPECT().Save(mock.Anything, garageID, mock.MatchedBy(func(s domain.Settings) bool {
			return s.SenderName == "Gara Anh Minh" && len(s.Off) == 1 && s.Off[domain.TypeVehicleReady]
		})).Return(nil).Once()

		settings, err := uc.UpdateSettings(t.Context(), garageID, "  Gara Anh Minh ",
			map[domain.Type]bool{domain.TypeVehicleReady: false, domain.TypeLowStock: true},
			[]domain.Type{domain.TypeVehicleReady, domain.TypeLowStock})
		require.NoError(t, err)
		assert.False(t, settings.Enabled(domain.TypeVehicleReady))
		assert.True(t, settings.Enabled(domain.TypeLowStock))
		assert.True(t, settings.Enabled(domain.TypeQuoteReady), "a type left out is on")
	})

	t.Run("refuses a long sender name, an unknown type and a repeated one", func(t *testing.T) {
		t.Parallel()

		uc, _ := newGarageUseCase(t)

		_, err := uc.UpdateSettings(t.Context(), garageID, strings.Repeat("a", 101), nil, nil)
		require.ErrorIs(t, err, usecase.ErrInvalidSenderName)

		_, err = uc.UpdateSettings(t.Context(), garageID, "", map[domain.Type]bool{}, []domain.Type{""})
		require.ErrorIs(t, err, usecase.ErrInvalidType)

		_, err = uc.UpdateSettings(t.Context(), garageID, "", map[domain.Type]bool{domain.TypeLowStock: true},
			[]domain.Type{domain.TypeLowStock, domain.TypeLowStock})
		require.ErrorIs(t, err, usecase.ErrInvalidType)
	})

	t.Run("a store failure is returned", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.Settings.EXPECT().Save(mock.Anything, garageID, mock.Anything).Return(errBoom).Once()

		_, err := uc.UpdateSettings(t.Context(), garageID, "", nil, nil)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestGetSettingsAndLog(t *testing.T) {
	t.Parallel()

	uc, d := newGarageUseCase(t)
	d.Settings.EXPECT().Get(mock.Anything, garageID).Return(domain.Settings{SenderName: "Gara"}, nil).Once()
	d.Log.EXPECT().List(mock.Anything, garageID, usecase.LogFilter{Type: domain.TypeLowStock}, 2, 10).
		Return([]domain.Log{{ID: "log-1"}}, 11, nil).Once()

	settings, err := uc.GetSettings(t.Context(), garageID)
	require.NoError(t, err)
	assert.Equal(t, "Gara", settings.SenderName)

	rows, total, err := uc.ListLog(t.Context(), garageID, usecase.LogFilter{Type: domain.TypeLowStock}, 2, 10)
	require.NoError(t, err)
	assert.Equal(t, 11, total)
	assert.Equal(t, "log-1", rows[0].ID)
}

func TestSuppress(t *testing.T) {
	t.Parallel()

	uc, d := newGarageUseCase(t)
	d.Suppressions.EXPECT().Suppress(mock.Anything, "bad@example.com", "bounce").Return(nil).Once()

	require.NoError(t, uc.Suppress(t.Context(), "  Bad@Example.com ", "bounce"))
	require.NoError(t, uc.Suppress(t.Context(), "  ", "bounce"), "an empty address is ignored")
}

func TestOptOut(t *testing.T) {
	t.Parallel()

	t.Run("a token signed for a customer opts them out inside the garage's transaction", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		token := optOutToken(t, uc)

		d.Tx.EXPECT().InGarage(mock.Anything, garageID, mock.Anything).
			RunAndReturn(func(ctx context.Context, _ string, fn func(context.Context) error) error { return fn(ctx) }).Once()
		d.Customers.EXPECT().OptOut(mock.Anything, garageID, customerID).Return(nil).Once()

		require.NoError(t, uc.OptOut(t.Context(), token))
	})

	t.Run("a tampered or malformed token is refused without touching anything", func(t *testing.T) {
		t.Parallel()

		uc, _ := newGarageUseCase(t)
		token := optOutToken(t, uc)

		for _, bad := range []string{"", "a.b", token[:len(token)-1] + "0", strings.Replace(token, customerID, "99999999-9999-9999-9999-999999999999", 1)} {
			require.ErrorIs(t, uc.OptOut(t.Context(), bad), usecase.ErrInvalidOptOutToken, bad)
		}

		_, _, ok := uc.VerifyOptOutToken(token)
		assert.True(t, ok)
	})

	t.Run("a customer who is gone makes the link invalid; a failure is returned", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		token := optOutToken(t, uc)

		d.Tx.EXPECT().InGarage(mock.Anything, garageID, mock.Anything).
			RunAndReturn(func(ctx context.Context, _ string, fn func(context.Context) error) error { return fn(ctx) }).Twice()
		d.Customers.EXPECT().OptOut(mock.Anything, garageID, customerID).Return(usecase.ErrCustomerNotFound).Once()
		d.Customers.EXPECT().OptOut(mock.Anything, garageID, customerID).Return(errBoom).Once()

		require.ErrorIs(t, uc.OptOut(t.Context(), token), usecase.ErrInvalidOptOutToken)
		require.ErrorIs(t, uc.OptOut(t.Context(), token), errBoom)
	})

	t.Run("without a key no token verifies", func(t *testing.T) {
		t.Parallel()

		keyless := usecase.NewNotificationUseCase(usecase.Deps{})
		_, _, ok := keyless.VerifyOptOutToken(garageID + "." + customerID + ".00")
		assert.False(t, ok)
	})
}

// optOutToken reads the signed token out of the link of a customer e-mail.
func optOutToken(t *testing.T, uc *usecase.NotificationUseCase) string {
	t.Helper()

	return uc.OptOutTokenForTest(garageID, customerID)
}
