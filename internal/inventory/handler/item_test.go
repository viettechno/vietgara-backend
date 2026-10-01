package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/handler"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

func sampleItem() *domain.Item {
	return &domain.Item{
		ID: itemID, GarageID: garageID, SKU: "OIL-5W30", Name: "Engine oil 5W-30", Unit: "L", ReorderThreshold: 5,
		QuantityOnHand: "10",
	}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 7)
	assert.NotNil(t, p[inventoryv1.InventoryService_CreateItem_FullMethodName].Garage)
}

func TestListItems(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().
		ListItems(anyArg, garageID, usecase.ListFilter{Limit: 20}).
		Return([]*domain.Item{sampleItem()}, 1, nil).Once()

	resp, err := e.srv.ListItems(signedIn(t), &inventoryv1.ListItemsRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Len(t, resp.GetData(), 1)
	assert.Equal(t, int32(1), resp.GetPagination().GetTotal())
}

func TestCreateItem(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().CreateItem(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.CreateItem(signedIn(t), &inventoryv1.CreateItemRequest{
		GarageId: garageID,
		Item:     &inventoryv1.Item{Sku: "OIL-5W30", Name: "Engine oil 5W-30", Unit: "L", ReorderThreshold: 5},
	})
	require.NoError(t, err)
	assert.Equal(t, "OIL-5W30", resp.GetItem().GetSku())
}

func TestGetItem(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().FindItemByID(anyArg, garageID, itemID).Return(sampleItem(), nil).Once()

	resp, err := e.srv.GetItem(signedIn(t), &inventoryv1.GetItemRequest{GarageId: garageID, ItemId: itemID})
	require.NoError(t, err)
	assert.Equal(t, itemID, resp.GetItem().GetId())

	e.repo.EXPECT().FindItemByID(anyArg, garageID, "missing").Return(nil, domain.ErrItemNotFound).Once()

	_, err = e.srv.GetItem(signedIn(t), &inventoryv1.GetItemRequest{GarageId: garageID, ItemId: "missing"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestUpdateItem(t *testing.T) {
	t.Parallel()

	t.Run("updates the given fields", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.repo.EXPECT().FindItemByID(anyArg, garageID, itemID).Return(sampleItem(), nil).Once()
		e.repo.EXPECT().UpdateItem(anyArg, anyArg).Return(nil).Once()

		resp, err := e.srv.UpdateItem(signedIn(t), &inventoryv1.UpdateItemRequest{
			GarageId: garageID, ItemId: itemID, Item: &inventoryv1.Item{ReorderThreshold: 10},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"reorder_threshold"}},
		})
		require.NoError(t, err)
		assert.Equal(t, int32(10), resp.GetItem().GetReorderThreshold())
	})

	t.Run("rejects an unknown mask path", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		_, err := e.srv.UpdateItem(signedIn(t), &inventoryv1.UpdateItemRequest{
			GarageId: garageID, ItemId: itemID, Item: &inventoryv1.Item{},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"quantity_on_hand"}},
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestDeleteItem(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().SoftDeleteItem(anyArg, garageID, itemID, actorID).Return(nil).Once()

	_, err := e.srv.DeleteItem(signedIn(t), &inventoryv1.DeleteItemRequest{GarageId: garageID, ItemId: itemID})
	require.NoError(t, err)
}
