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
	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 15)
	assert.Equal(t, access.InventoryRead, p[inventoryv1.InventoryService_ListStockLevels_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryWrite, p[inventoryv1.InventoryService_CompleteGoodsReceipt_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryWrite, p[inventoryv1.InventoryService_PostStockIssue_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryCostRead, p[inventoryv1.InventoryService_GetPurchasesReport_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryCostRead, p[inventoryv1.InventoryService_GetStockValuationReport_FullMethodName].Garage.Permission)
}

func TestListStockLevels(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.parts.EXPECT().SearchParts(anyArg, garageID, usecase.PartQuery{Query: "beer", CategoryID: "c", Brand: "Tiger", Limit: 20, IncludeInactive: true}).
		Return([]usecase.PartInfo{samplePart()}, 1, nil).Once()
	e.repo.EXPECT().Balances(anyArg, garageID, []string{partID}).Return(map[string]usecase.Balance{partID: {OnHand: "3"}}, nil).Once()

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
	e.repo.EXPECT().Balances(anyArg, garageID, []string{partID}).Return(map[string]usecase.Balance{partID: {OnHand: "40", Reserved: "10", AverageCost: "100.5"}}, nil).Once()

	resp, err := e.srv.GetStockLevel(signedIn(t), &inventoryv1.GetStockLevelRequest{GarageId: garageID, PartId: partID})
	require.NoError(t, err)
	assert.Equal(t, "40", resp.GetStockLevel().GetQuantityOnHand())
	assert.Equal(t, "10", resp.GetStockLevel().GetQuantityReserved())
	assert.Equal(t, "30", resp.GetStockLevel().GetQuantityAvailable())
	assert.Nil(t, resp.GetStockLevel().AverageCost, "cost is omitted without inventory.cost.read")
	assert.Nil(t, resp.GetStockLevel().StockValue)

	e.parts.EXPECT().Parts(anyArg, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
	e.repo.EXPECT().Balances(anyArg, garageID, []string{partID}).Return(map[string]usecase.Balance{partID: {OnHand: "40", Reserved: "10", AverageCost: "100.5"}}, nil).Once()

	resp, err = e.srv.GetStockLevel(signedInWithCost(t), &inventoryv1.GetStockLevelRequest{GarageId: garageID, PartId: partID})
	require.NoError(t, err)
	assert.Equal(t, int64(101), resp.GetStockLevel().GetAverageCost(), "rounded to whole dong")
	assert.Equal(t, int64(4020), resp.GetStockLevel().GetStockValue(), "40 x 100.5")

	e.parts.EXPECT().Parts(anyArg, garageID, []string{partID}).Return(nil, nil).Once()

	_, err = e.srv.GetStockLevel(signedIn(t), &inventoryv1.GetStockLevelRequest{GarageId: garageID, PartId: partID})
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
			GarageId: garageID, PartId: partID, Type: in,
		})
		require.NoError(t, err)
		require.Len(t, resp.GetData(), 2)
		assert.Equal(t, int32(2), resp.GetPagination().GetTotal())
		assert.Equal(t, "BEER-330", resp.GetData()[0].GetPartSku())
		assert.Nil(t, resp.GetData()[0].UnitCost, "cost is omitted without inventory.cost.read")
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
