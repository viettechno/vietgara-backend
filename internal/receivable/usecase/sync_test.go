package usecase_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func event(t *testing.T, garage string, payload any) outbox.Event {
	t.Helper()

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	return outbox.Event{ID: "e-1", GarageID: garage, Data: data}
}

func changed(t *testing.T) outbox.Event {
	t.Helper()

	return event(t, garageID, map[string]string{"settlement_id": settlementID})
}

func settlementView() usecase.SettlementView {
	created := time.Date(2026, time.November, 9, 20, 0, 0, 0, time.UTC) // the 10th in Ho Chi Minh City
	voided := created.Add(48 * time.Hour)

	return usecase.SettlementView{
		ID: settlementID, Number: "QT-0001", CustomerID: customerID, CustomerName: "An", VehicleLicensePlate: "51A-111.11",
		TotalAmount: 1_000_000, PaidAmount: 400_000, CreatedAt: created,
		Payments: []usecase.PaymentView{
			{ID: "p-1", VoucherNumber: "PT-1", Amount: 400_000, Method: "cash", CreatedAt: created.Add(time.Hour)},
			{ID: "p-2", VoucherNumber: "PT-2", Amount: 300_000, Method: "bank_transfer", CreatedAt: created.Add(2 * time.Hour), VoidedAt: &voided},
		},
	}
}

func TestReceivableUseCase_HandleSettlementChanged(t *testing.T) {
	t.Parallel()

	t.Run("creates the receivable with its due date and the whole ledger", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.lock()
		d.timezone()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(settlementView(), nil).Once()
		d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(nil, domain.ErrReceivableNotFound).Once()
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()

		var (
			saved   *domain.Receivable
			entries []domain.Entry
		)

		d.Repo.EXPECT().Save(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, r *domain.Receivable) error {
			saved, r.ID = r, receivableID

			return nil
		}).Once()
		d.Repo.EXPECT().AddEntries(mock.Anything, garageID, receivableID, mock.Anything).
			Run(func(_ context.Context, _, _ string, e []domain.Entry) { entries = e }).Return(nil).Once()

		require.NoError(t, uc.HandleSettlementChanged(t.Context(), changed(t)))

		require.Equal(t, day(2026, 11, 17), saved.DueDate, "the 10th in the garage's zone plus 7 days")
		require.Equal(t, domain.StatusOpen, saved.Status)
		require.EqualValues(t, 400_000, saved.PaidAmount)
		require.Equal(t, customerID, saved.CustomerID)
		require.Equal(t, "QT-0001", saved.SettlementNumber)

		keys := make([]string, len(entries))
		for i, e := range entries {
			keys[i] = e.SourceKey
		}

		require.Equal(t, []string{"charge:" + settlementID, "payment:p-1", "payment:p-2", "void:p-2"}, keys)
		require.Equal(t, domain.EntryVoid, entries[3].Kind)
		require.Equal(t, "PT-2", entries[3].Reference)
	})

	t.Run("updates an existing receivable and keeps its due date and customer", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.lock()

		view := settlementView()
		view.CustomerID, view.PaidAmount = "", 1_000_000

		existing := openReceivable(day(2026, 11, 17))

		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(view, nil).Once()
		d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(existing, nil).Once()
		d.Repo.EXPECT().Save(mock.Anything, existing).Return(nil).Once()
		d.Repo.EXPECT().AddEntries(mock.Anything, garageID, receivableID, mock.Anything).Return(nil).Once()

		require.NoError(t, uc.Sync(t.Context(), garageID, settlementID))
		require.Equal(t, domain.StatusPaid, existing.Status)
		require.Equal(t, customerID, existing.CustomerID, "a settlement that no longer resolves its customer keeps the known one")
		require.Equal(t, day(2026, 11, 17), existing.DueDate)
	})

	t.Run("a settlement with nothing to collect has no charge line", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.lock()
		d.timezone()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(usecase.SettlementView{
			ID: settlementID, Number: "QT-0002", CreatedAt: now,
		}, nil).Once()
		d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(nil, domain.ErrReceivableNotFound).Once()
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()
		d.Repo.EXPECT().Save(mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, r *domain.Receivable) error {
			require.Equal(t, domain.StatusPaid, r.Status)
			r.ID = receivableID

			return nil
		}).Once()
		d.Repo.EXPECT().AddEntries(mock.Anything, garageID, receivableID, mock.MatchedBy(func(e []domain.Entry) bool { return len(e) == 0 })).Return(nil).Once()

		require.NoError(t, uc.Sync(t.Context(), garageID, settlementID))
	})
}

func TestReceivableUseCase_HandleSettlementChanged_Part2(t *testing.T) {
	t.Parallel()

	t.Run("an event of a settlement that is gone is dropped; any other failure is returned", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.lock()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(usecase.SettlementView{}, apperror.New(apperror.CodeNotFound, "gone")).Once()
		require.NoError(t, uc.HandleSettlementChanged(t.Context(), changed(t)))

		uc, d = newUseCase(t)
		d.lock()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(usecase.SettlementView{}, errBoom).Once()
		require.ErrorIs(t, uc.HandleSettlementChanged(t.Context(), changed(t)), errBoom)
	})

	t.Run("a malformed event is dropped", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		require.NoError(t, uc.HandleSettlementChanged(t.Context(), outbox.Event{GarageID: garageID, Data: []byte("not json")}))
		require.NoError(t, uc.HandleSettlementChanged(t.Context(), event(t, garageID, map[string]string{})))
		require.NoError(t, uc.HandleSettlementChanged(t.Context(), event(t, "", map[string]string{"settlement_id": settlementID})))
	})

	t.Run("each failure is returned so the event is delivered again", func(t *testing.T) {
		t.Parallel()

		// reading the settlement
		uc, d := newUseCase(t)
		d.lock()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(usecase.SettlementView{}, errBoom).Once()
		require.ErrorIs(t, uc.Sync(t.Context(), garageID, settlementID), errBoom)

		// looking the receivable up
		uc, d = newUseCase(t)
		d.lock()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(settlementView(), nil).Once()
		d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(nil, errBoom).Once()
		require.ErrorIs(t, uc.Sync(t.Context(), garageID, settlementID), errBoom)

		// the settings
		uc, d = newUseCase(t)
		d.lock()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(settlementView(), nil).Once()
		d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(nil, domain.ErrReceivableNotFound).Once()
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.Settings{}, errBoom).Once()
		require.ErrorIs(t, uc.Sync(t.Context(), garageID, settlementID), errBoom)

		// the time zone
		uc, d = newUseCase(t)
		d.lock()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(settlementView(), nil).Once()
		d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(nil, domain.ErrReceivableNotFound).Once()
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoom).Once()
		require.ErrorIs(t, uc.Sync(t.Context(), garageID, settlementID), errBoom)

		// saving
		uc, d = newUseCase(t)
		d.lock()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(settlementView(), nil).Once()
		d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(openReceivable(day(2026, 11, 17)), nil).Once()
		d.Repo.EXPECT().Save(mock.Anything, mock.Anything).Return(errBoom).Once()
		require.ErrorIs(t, uc.Sync(t.Context(), garageID, settlementID), errBoom)

		// the ledger
		uc, d = newUseCase(t)
		d.lock()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(settlementView(), nil).Once()
		d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(openReceivable(day(2026, 11, 17)), nil).Once()
		d.Repo.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().AddEntries(mock.Anything, garageID, receivableID, mock.Anything).Return(errBoom).Once()
		require.ErrorIs(t, uc.Sync(t.Context(), garageID, settlementID), errBoom)
	})
}

func TestReceivableUseCase_Sync_Lock(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().LockSettlement(mock.Anything, settlementID).Return(errBoom).Once()

	require.ErrorIs(t, uc.Sync(t.Context(), garageID, settlementID), errBoom, "nothing is read before the lock is held")
}
