package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

func TestCreateStockReceipt(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	item := sampleItem()
	updated := sampleItem()
	updated.QuantityOnHand = "15"

	e.repo.EXPECT().FindItemByID(anyArg, garageID, itemID).Return(item, nil).Once()
	e.repo.EXPECT().RecordTransaction(anyArg, anyArg).Return(nil).Once()
	e.repo.EXPECT().FindItemByID(anyArg, garageID, itemID).Return(updated, nil).Once()

	resp, err := e.srv.CreateStockReceipt(signedIn(t), &inventoryv1.CreateStockReceiptRequest{
		GarageId: garageID, ItemId: itemID, Quantity: "5", UnitCost: 50_000,
	})
	require.NoError(t, err)
	assert.Equal(t, "15", resp.GetItem().GetQuantityOnHand())
	assert.Equal(t, int64(50_000), resp.GetTransaction().GetUnitCost())
}

func TestListStockTransactions(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().
		ListTransactions(anyArg, garageID, usecase.TransactionFilter{ItemID: itemID, Limit: 20}).
		Return([]*domain.StockTransaction{{ID: "tx-1", ItemID: itemID, Type: domain.TransactionIn, Quantity: "5"}}, 1, nil).
		Once()

	resp, err := e.srv.ListStockTransactions(signedIn(t), &inventoryv1.ListStockTransactionsRequest{
		GarageId: garageID, ItemId: itemID,
	})
	require.NoError(t, err)
	assert.Len(t, resp.GetData(), 1)
	assert.Equal(t, int32(1), resp.GetPagination().GetTotal())
}
