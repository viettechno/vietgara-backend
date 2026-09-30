package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestInventoryUseCase_CreateItem(t *testing.T) {
	t.Parallel()

	t.Run("adds a part code", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().CreateItem(mock.Anything, mock.Anything).Return(nil).Once()

		item, err := uc.CreateItem(t.Context(), usecase.CreateItemRequest{
			GarageID: garageID, ActorID: actorID,
			Fields: usecase.ItemFields{SKU: new("OIL-5W30"), Name: new("Engine oil 5W-30"), Unit: new("L")},
		})
		require.NoError(t, err)
		require.Equal(t, "OIL-5W30", item.SKU)
		require.Equal(t, "0", item.QuantityOnHand)
	})

	t.Run("SKU required", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := uc.CreateItem(t.Context(), usecase.CreateItemRequest{
			GarageID: garageID, Fields: usecase.ItemFields{Name: new("Engine oil")},
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("name required", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := uc.CreateItem(t.Context(), usecase.CreateItemRequest{
			GarageID: garageID, Fields: usecase.ItemFields{SKU: new("OIL-5W30")},
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().CreateItem(mock.Anything, mock.Anything).Return(domain.ErrSKUTaken).Once()

		_, err := uc.CreateItem(t.Context(), usecase.CreateItemRequest{
			GarageID: garageID, Fields: usecase.ItemFields{SKU: new("OIL-5W30"), Name: new("Engine oil")},
		})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})
}

func TestInventoryUseCase_GetItem(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(sampleItem(), nil).Once()

	item, err := uc.GetItem(t.Context(), garageID, itemID)
	require.NoError(t, err)
	require.Equal(t, itemID, item.ID)

	d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrItemNotFound).Once()

	_, err = uc.GetItem(t.Context(), garageID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestInventoryUseCase_UpdateItem(t *testing.T) {
	t.Parallel()

	t.Run("applies partial updates", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(sampleItem(), nil).Once()
		d.Repo.EXPECT().UpdateItem(mock.Anything, mock.Anything).Return(nil).Once()

		item, err := uc.UpdateItem(t.Context(), usecase.UpdateItemRequest{
			GarageID: garageID, ItemID: itemID, ActorID: actorID,
			Fields: usecase.ItemFields{ReorderThreshold: new(10)},
		})
		require.NoError(t, err)
		require.Equal(t, 10, item.ReorderThreshold)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrItemNotFound).Once()

		_, err := uc.UpdateItem(t.Context(), usecase.UpdateItemRequest{GarageID: garageID, ItemID: "missing"})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("invalid reorder threshold", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(sampleItem(), nil).Once()

		_, err := uc.UpdateItem(t.Context(), usecase.UpdateItemRequest{
			GarageID: garageID, ItemID: itemID, Fields: usecase.ItemFields{ReorderThreshold: new(-1)},
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}

func TestInventoryUseCase_ListItems(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	want := []*domain.Item{sampleItem()}
	d.Repo.EXPECT().
		ListItems(mock.Anything, garageID, usecase.ListFilter{Query: "oil", Limit: 20}).
		Return(want, 1, nil).Once()

	page, err := uc.ListItems(t.Context(), usecase.ListItemsRequest{GarageID: garageID, Query: " oil "})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, want, page.Items)

	d.Repo.EXPECT().ListItems(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

	_, err = uc.ListItems(t.Context(), usecase.ListItemsRequest{GarageID: garageID})
	assertAppError(t, err, apperror.CodeInternal)
}

func TestInventoryUseCase_DeleteItem(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().SoftDeleteItem(mock.Anything, garageID, itemID, actorID).Return(nil).Once()

	require.NoError(t, uc.DeleteItem(t.Context(), garageID, itemID, actorID))

	d.Repo.EXPECT().SoftDeleteItem(mock.Anything, garageID, "missing", actorID).Return(domain.ErrItemNotFound).Once()

	err := uc.DeleteItem(t.Context(), garageID, "missing", actorID)
	assertAppError(t, err, apperror.CodeNotFound)
}
