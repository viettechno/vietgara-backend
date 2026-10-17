package usecase_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestInventoryUseCase_GetStockLevel(t *testing.T) {
	t.Parallel()

	t.Run("part with stock", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).Return(map[string]usecase.Balance{partID: {OnHand: "12"}}, nil).Once()

		level, err := uc.GetStockLevel(t.Context(), garageID, partID)
		require.NoError(t, err)
		assert.Equal(t, "12", level.QuantityOnHand)
		assert.False(t, level.LowStock)
		assert.Equal(t, "can", level.BaseUnitAbbreviation)
	})

	t.Run("part without movements has zero stock and is low", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).Return(map[string]usecase.Balance{}, nil).Once()

		level, err := uc.GetStockLevel(t.Context(), garageID, partID)
		require.NoError(t, err)
		assert.Equal(t, "0", level.QuantityOnHand)
		assert.True(t, level.LowStock)
	})

	t.Run("unknown part", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, nil).Once()

		_, err := uc.GetStockLevel(t.Context(), garageID, partID)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("malformed id is not found without asking the catalogue", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := uc.GetStockLevel(t.Context(), garageID, "not-a-uuid")
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("catalogue failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, errBoom).Once()

		_, err := uc.GetStockLevel(t.Context(), garageID, partID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("balance failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).Return(nil, errBoom).Once()

		_, err := uc.GetStockLevel(t.Context(), garageID, partID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_ListStockLevels(t *testing.T) {
	t.Parallel()

	t.Run("composes the catalogue page with the balances", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		other := samplePart()
		other.ID, other.SKU = "other", "OIL"

		d.Parts.EXPECT().
			SearchParts(mock.Anything, garageID, usecase.PartQuery{Query: "b", CategoryID: "cat", Brand: "Tiger", Limit: 20}).
			Return([]usecase.PartInfo{samplePart(), other}, 2, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID, "other"}).Return(map[string]usecase.Balance{partID: {OnHand: "9"}}, nil).Once()

		page, err := uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{
			GarageID: garageID, Query: "b", CategoryID: "cat", Brand: "Tiger",
		})
		require.NoError(t, err)
		require.Len(t, page.Items, 2)
		assert.Equal(t, 2, page.Total)
		assert.Equal(t, "9", page.Items[0].QuantityOnHand)
		assert.Equal(t, "0", page.Items[1].QuantityOnHand)
	})

	t.Run("passes the page window to the catalogue", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, usecase.PartQuery{Limit: 10, Offset: 20, IncludeInactive: true}).
			Return(nil, 0, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{}).Return(nil, nil).Once()

		page, err := uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{
			GarageID: garageID, PageRequest: usecase.PageRequest{Page: 3, PageSize: 10}, IncludeInactive: true,
		})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
	})

	t.Run("catalogue and balance failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err := uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, 1, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_ListStockLevels_LowStock(t *testing.T) {
	t.Parallel()

	parts := func(from, n int) []usecase.PartInfo {
		out := make([]usecase.PartInfo, 0, n)
		for i := from; i < from+n; i++ {
			p := samplePart()
			p.ID, p.ReorderThreshold = fmt.Sprintf("p%d", i), "5"
			out = append(out, p)
		}

		return out
	}

	t.Run("keeps only parts at or below the threshold and pages the result", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		// 3 parts: p0 has 1 (low), p1 has 5 (low), p2 has 6 (fine).
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).
			Return(map[string]usecase.Balance{"p0": {OnHand: "1"}, "p1": {OnHand: "5"}, "p2": {OnHand: "6"}}, nil).Once()
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, usecase.PartQuery{Limit: usecase.MaxPageSize}).
			Return(parts(0, 3), 3, nil).Once()

		page, err := uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{
			GarageID: garageID, LowStock: true, PageRequest: usecase.PageRequest{Page: 1, PageSize: 1},
		})
		require.NoError(t, err)
		assert.Equal(t, 2, page.Total)
		require.Len(t, page.Items, 1)
		assert.Equal(t, "p0", page.Items[0].PartID)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).Return(map[string]usecase.Balance{"p0": {OnHand: "1"}, "p1": {OnHand: "5"}}, nil).Once()
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, mock.Anything).Return(parts(0, 3), 3, nil).Once()

		page, err = uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{
			GarageID: garageID, LowStock: true, PageRequest: usecase.PageRequest{Page: 2, PageSize: 1},
		})
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		assert.Equal(t, "p1", page.Items[0].PartID)
	})

	t.Run("a page beyond the result is empty", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).Return(nil, nil).Once()
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, mock.Anything).Return(parts(0, 1), 1, nil).Once()

		page, err := uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{
			GarageID: garageID, LowStock: true, PageRequest: usecase.PageRequest{Page: 5, PageSize: 20},
		})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
		assert.Equal(t, 1, page.Total)
	})

	t.Run("scans every catalogue page", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).Return(nil, nil).Once()
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, usecase.PartQuery{Limit: 100, Offset: 0}).
			Return(parts(0, 100), 150, nil).Once()
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, usecase.PartQuery{Limit: 100, Offset: 100}).
			Return(parts(100, 50), 150, nil).Once()

		page, err := uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{GarageID: garageID, LowStock: true})
		require.NoError(t, err)
		assert.Equal(t, 150, page.Total)
		assert.Len(t, page.Items, 20)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).Return(nil, errBoom).Once()

		_, err := uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{GarageID: garageID, LowStock: true})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).Return(nil, nil).Once()
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err = uc.ListStockLevels(t.Context(), usecase.ListStockLevelsRequest{GarageID: garageID, LowStock: true})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_ListTransactions(t *testing.T) {
	t.Parallel()

	t.Run("describes the part and the unit", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		unit := unitID
		entered := "2"
		txs := []*domain.StockTransaction{
			{ID: "t1", PartID: partID, Type: domain.TransactionIn, Quantity: "48", UnitID: &unit, EnteredQuantity: &entered},
			{ID: "t2", PartID: partID, Type: domain.TransactionOut, Quantity: "1"},
		}

		d.Repo.EXPECT().ListTransactions(mock.Anything, garageID, usecase.TransactionFilter{PartID: partID, Type: domain.TransactionIn, Limit: 20}).
			Return(txs, 2, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, []string{unitID}).Return(map[string]string{unitID: "case"}, nil).Once()

		page, err := uc.ListTransactions(t.Context(), usecase.ListTransactionsRequest{
			GarageID: garageID, PartID: partID, Type: domain.TransactionIn,
		})
		require.NoError(t, err)
		assert.Equal(t, 2, page.Total)
		assert.Equal(t, "Beer 330 ml", page.Items[0].PartName)
		assert.Equal(t, "case", page.Items[0].UnitAbbreviation)
		assert.Empty(t, page.Items[1].UnitAbbreviation)
	})

	t.Run("empty page asks the catalogue nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListTransactions(mock.Anything, garageID, mock.Anything).Return(nil, 0, nil).Once()

		page, err := uc.ListTransactions(t.Context(), usecase.ListTransactionsRequest{GarageID: garageID})
		require.NoError(t, err)
		assert.Empty(t, page.Items)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListTransactions(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err := uc.ListTransactions(t.Context(), usecase.ListTransactionsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().ListTransactions(mock.Anything, garageID, mock.Anything).
			Return([]*domain.StockTransaction{{PartID: partID}}, 1, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.ListTransactions(t.Context(), usecase.ListTransactionsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		unit := unitID
		d.Repo.EXPECT().ListTransactions(mock.Anything, garageID, mock.Anything).
			Return([]*domain.StockTransaction{{PartID: partID, UnitID: &unit}}, 1, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.ListTransactions(t.Context(), usecase.ListTransactionsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}
