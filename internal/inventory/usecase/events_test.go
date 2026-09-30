package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

func TestInventoryUseCase_HandleRepairOrderClosed(t *testing.T) {
	t.Parallel()

	t.Run("issues stock for each part line", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(sampleItem(), nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, mock.Anything, garageID, "", mock.Anything).Return(nil).Once()

		payload := []byte(`{"repair_order_id":"ro-1","part_lines":[{"item_ref_id":"` + itemID + `","quantity":"2"}]}`)

		err := uc.HandleRepairOrderClosed(t.Context(), outbox.Event{GarageID: garageID, Data: payload})
		require.NoError(t, err)
	})

	t.Run("skips a line without an item reference", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)
		payload := []byte(`{"repair_order_id":"ro-1","part_lines":[{"item_ref_id":"","quantity":"2"}]}`)

		err := uc.HandleRepairOrderClosed(t.Context(), outbox.Event{GarageID: garageID, Data: payload})
		require.NoError(t, err)
	})

	t.Run("logs and continues past a line that fails to issue", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, "missing-item").Return(nil, domain.ErrItemNotFound).Once()

		payload := []byte(`{"repair_order_id":"ro-1","part_lines":[{"item_ref_id":"missing-item","quantity":"2"}]}`)

		err := uc.HandleRepairOrderClosed(t.Context(), outbox.Event{GarageID: garageID, Data: payload})
		require.NoError(t, err)
	})

	t.Run("drops a malformed payload", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		err := uc.HandleRepairOrderClosed(t.Context(), outbox.Event{GarageID: garageID, Data: []byte(`not json`)})
		require.NoError(t, err)
	})

	t.Run("drops a payload missing the repair order id", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		err := uc.HandleRepairOrderClosed(t.Context(), outbox.Event{GarageID: garageID, Data: []byte(`{"part_lines":[]}`)})
		require.NoError(t, err)
	})
}
