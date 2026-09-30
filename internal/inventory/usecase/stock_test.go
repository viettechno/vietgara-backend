package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestInventoryUseCase_CreateStockReceipt(t *testing.T) {
	t.Parallel()

	t.Run("records a goods receipt", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		item := sampleItem()
		updated := sampleItem()
		updated.QuantityOnHand = "15"

		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(item, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Type == domain.TransactionIn && tx.Quantity == "5" && tx.RefType == domain.RefPurchase
		})).Return(nil).Once()
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(updated, nil).Once()

		result, err := uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{
			GarageID: garageID, ActorID: actorID, ItemID: itemID, Quantity: "5", UnitCost: 50_000,
		})
		require.NoError(t, err)
		require.Equal(t, "15", result.Item.QuantityOnHand)
		require.Equal(t, int64(50_000), *result.Transaction.UnitCost)
	})

	t.Run("item not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrItemNotFound).Once()

		_, err := uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{GarageID: garageID, ItemID: "missing"})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("invalid quantity", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(sampleItem(), nil).Once()

		_, err := uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{
			GarageID: garageID, ItemID: itemID, Quantity: "not-a-number",
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("record transaction failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(sampleItem(), nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{
			GarageID: garageID, ItemID: itemID, Quantity: "5",
		})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_ListTransactions(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	want := []*domain.StockTransaction{{ID: "tx-1"}}
	d.Repo.EXPECT().
		ListTransactions(mock.Anything, garageID, usecase.TransactionFilter{ItemID: itemID, Limit: 20}).
		Return(want, 1, nil).Once()

	page, err := uc.ListTransactions(t.Context(), usecase.ListTransactionsRequest{GarageID: garageID, ItemID: itemID})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, want, page.Items)
}

func TestInventoryUseCase_IssueStock(t *testing.T) {
	t.Parallel()

	t.Run("deducts stock and publishes stock.issued", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(sampleItem(), nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Type == domain.TransactionOut && tx.RefType == domain.RefRepairOrder && *tx.RefID == "ro-1"
		})).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventStockIssued, garageID, "", mock.Anything).Return(nil).Once()

		require.NoError(t, uc.IssueStock(t.Context(), garageID, itemID, "2", "ro-1"))
	})

	t.Run("already issued is a no-op", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(sampleItem(), nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(domain.ErrAlreadyIssued).Once()

		require.NoError(t, uc.IssueStock(t.Context(), garageID, itemID, "2", "ro-1"))
	})

	t.Run("unknown item", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrItemNotFound).Once()

		err := uc.IssueStock(t.Context(), garageID, "missing", "2", "ro-1")
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("publish failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindItemByID(mock.Anything, garageID, itemID).Return(sampleItem(), nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventStockIssued, garageID, "", mock.Anything).Return(errBoom).Once()

		err := uc.IssueStock(t.Context(), garageID, itemID, "2", "ro-1")
		require.ErrorIs(t, err, errBoom)
	})
}
