package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/handler"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 4)
	assert.NotNil(t, p[inventoryv1.InventoryService_ListStockLevels_FullMethodName].Garage)
	assert.NotNil(t, p[inventoryv1.InventoryService_CreateStockReceipt_FullMethodName].Garage)
}

func TestListStockLevels(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.parts.EXPECT().SearchParts(anyArg, garageID, usecase.PartQuery{Query: "beer", CategoryID: "c", Brand: "Tiger", Limit: 20, IncludeInactive: true}).
		Return([]usecase.PartInfo{samplePart()}, 1, nil).Once()
	e.repo.EXPECT().Balances(anyArg, garageID, []string{partID}).Return(map[string]string{partID: "3"}, nil).Once()

	resp, err := e.srv.ListStockLevels(signedIn(t), &inventoryv1.ListStockLevelsRequest{
		GarageId: garageID, Query: "beer", CategoryId: "c", Brand: "Tiger", IncludeInactive: true,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetData(), 1)

	level := resp.GetData()[0]
	assert.Equal(t, partID, level.GetPartId())
	assert.Equal(t, "3", level.GetQuantityOnHand())
	assert.True(t, level.GetLowStock())
	assert.Equal(t, "https://cdn/x.png", level.GetThumbnailUrl())
	assert.Equal(t, "can", level.GetBaseUnitAbbreviation())
	assert.Equal(t, int32(1), resp.GetPagination().GetTotal())
}

func TestListStockLevels_Failure(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.parts.EXPECT().SearchParts(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()

	_, err := e.srv.ListStockLevels(signedIn(t), &inventoryv1.ListStockLevelsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestGetStockLevel(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.parts.EXPECT().Parts(anyArg, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
	e.repo.EXPECT().Balances(anyArg, garageID, []string{partID}).Return(map[string]string{partID: "40"}, nil).Once()

	resp, err := e.srv.GetStockLevel(signedIn(t), &inventoryv1.GetStockLevelRequest{GarageId: garageID, PartId: partID})
	require.NoError(t, err)
	assert.Equal(t, "40", resp.GetStockLevel().GetQuantityOnHand())

	e.parts.EXPECT().Parts(anyArg, garageID, []string{partID}).Return(nil, nil).Once()

	_, err = e.srv.GetStockLevel(signedIn(t), &inventoryv1.GetStockLevelRequest{GarageId: garageID, PartId: partID})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestCreateStockReceipt(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	unit, entered := unitID, "2"

	e.parts.EXPECT().Parts(anyArg, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
	e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, unitID, "2").
		Return(usecase.ResolvedQuantity{Base: "48", UnitID: &unit, Entered: &entered}, nil).Once()
	e.repo.EXPECT().RecordTransaction(anyArg, anyArg).Return(nil).Once()
	e.parts.EXPECT().UnitAbbreviations(anyArg, garageID, []string{unitID}).Return(map[string]string{unitID: "case"}, nil).Once()
	e.repo.EXPECT().Balances(anyArg, garageID, []string{partID}).Return(map[string]string{partID: "48"}, nil).Once()

	resp, err := e.srv.CreateStockReceipt(signedIn(t), &inventoryv1.CreateStockReceiptRequest{
		GarageId: garageID, ItemId: partID, Quantity: "2", UnitCost: 120_000, UnitId: &unit,
	})
	require.NoError(t, err)
	assert.Equal(t, "48", resp.GetStockLevel().GetQuantityOnHand())
	assert.Equal(t, "48", resp.GetTransaction().GetQuantity())
	assert.Equal(t, "2", resp.GetTransaction().GetEnteredQuantity())
	assert.Equal(t, "case", resp.GetTransaction().GetUnitAbbreviation())
	assert.Equal(t, unitID, resp.GetTransaction().GetUnitId())
	// 120,000 per case of 24 is 5,000 per can.
	assert.Equal(t, int64(5_000), resp.GetTransaction().GetUnitCost())
}

func TestCreateStockReceipt_UnknownPart(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.parts.EXPECT().Parts(anyArg, garageID, []string{partID}).Return(nil, nil).Once()

	_, err := e.srv.CreateStockReceipt(signedIn(t), &inventoryv1.CreateStockReceiptRequest{GarageId: garageID, ItemId: partID, Quantity: "1"})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestListStockTransactions(t *testing.T) {
	t.Parallel()

	cost := "5000.4"
	ref := "ro-1"

	cases := map[inventoryv1.StockTransactionType]domain.TransactionType{
		inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_IN:          domain.TransactionIn,
		inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_OUT:         domain.TransactionOut,
		inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_UNSPECIFIED: "",
	}

	for in, want := range cases {
		e := newEnv(t)
		e.repo.EXPECT().ListTransactions(anyArg, garageID, usecase.TransactionFilter{PartID: partID, Type: want, Limit: 20}).
			Return([]*domain.StockTransaction{
				{ID: "tx-1", PartID: partID, Type: domain.TransactionIn, Quantity: "5", UnitCost: &cost, RefType: domain.RefPurchase},
				{ID: "tx-2", PartID: partID, Type: domain.TransactionOut, Quantity: "1", RefType: domain.RefRepairOrder, RefID: &ref},
			}, 2, nil).Once()
		e.parts.EXPECT().Parts(anyArg, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()

		resp, err := e.srv.ListStockTransactions(signedIn(t), &inventoryv1.ListStockTransactionsRequest{
			GarageId: garageID, ItemId: partID, Type: in,
		})
		require.NoError(t, err)
		require.Len(t, resp.GetData(), 2)
		assert.Equal(t, int32(2), resp.GetPagination().GetTotal())
		assert.Equal(t, "BEER-330", resp.GetData()[0].GetItemSku())
		assert.Equal(t, int64(5_000), resp.GetData()[0].GetUnitCost(), "cost is rounded to whole dong")
		assert.Equal(t, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_PURCHASE, resp.GetData()[0].GetRefType())
		assert.Equal(t, inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_OUT, resp.GetData()[1].GetType())
		assert.Equal(t, "ro-1", resp.GetData()[1].GetRefId())
		assert.Equal(t, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_REPAIR_ORDER, resp.GetData()[1].GetRefType())
	}
}

func TestListStockTransactions_Failure(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().ListTransactions(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()

	_, err := e.srv.ListStockTransactions(signedIn(t), &inventoryv1.ListStockTransactionsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))
}
