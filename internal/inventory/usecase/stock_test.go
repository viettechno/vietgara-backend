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
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).Return(map[string]string{partID: "12"}, nil).Once()

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
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).Return(map[string]string{}, nil).Once()

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
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID, "other"}).Return(map[string]string{partID: "9"}, nil).Once()

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
			Return(map[string]string{"p0": "1", "p1": "5", "p2": "6"}, nil).Once()
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
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string(nil)).Return(map[string]string{"p0": "1", "p1": "5"}, nil).Once()
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

//nolint:funlen // one table of receipt cases
func TestInventoryUseCase_CreateStockReceipt(t *testing.T) {
	t.Parallel()

	expectPart := func(d *deps) {
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
	}

	t.Run("base unit with cost", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectPart(d)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "10").
			Return(usecase.ResolvedQuantity{Base: "10"}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Type == domain.TransactionIn && tx.RefType == domain.RefPurchase && tx.Quantity == "10" &&
				tx.UnitID == nil && tx.EnteredQuantity == nil && *tx.UnitCost == "5000" && *tx.CreatedBy == actorID
		})).Return(nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).Return(map[string]string{partID: "10"}, nil).Once()

		result, err := uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{
			GarageID: garageID, ActorID: actorID, PartID: partID, Quantity: "10", UnitCost: 5_000,
		})
		require.NoError(t, err)
		assert.Equal(t, "10", result.StockLevel.QuantityOnHand)
		assert.Equal(t, "BEER-330", result.Transaction.PartSKU)
		assert.Equal(t, "can", result.Transaction.BaseUnitAbbreviation)
	})

	t.Run("packaging unit converts the quantity and the cost", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectPart(d)

		unit, entered := unitID, "10"
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "10").
			Return(usecase.ResolvedQuantity{Base: "240", UnitID: &unit, Entered: &entered}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Quantity == "240" && *tx.UnitID == unitID && *tx.EnteredQuantity == "10" && *tx.UnitCost == "5000"
		})).Return(nil).Once()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, []string{unitID}).
			Return(map[string]string{unitID: "case"}, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).Return(map[string]string{partID: "240"}, nil).Once()

		result, err := uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{
			GarageID: garageID, PartID: partID, Quantity: "10", UnitID: unitID, UnitCost: 120_000,
		})
		require.NoError(t, err)
		assert.Equal(t, "case", result.Transaction.UnitAbbreviation)
		assert.Equal(t, "240", result.StockLevel.QuantityOnHand)
	})

	t.Run("no cost recorded", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectPart(d)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "1").Return(usecase.ResolvedQuantity{Base: "1"}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.UnitCost == nil && tx.CreatedBy == nil
		})).Return(nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()

		_, err := uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{GarageID: garageID, PartID: partID, Quantity: "1"})
		require.NoError(t, err)
	})

	t.Run("rejections", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, nil).Once()

		_, err := uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{GarageID: garageID, PartID: partID, Quantity: "1"})
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		inactive := samplePart()
		inactive.IsActive = false
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{inactive}, nil).Once()

		_, err = uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{GarageID: garageID, PartID: partID, Quantity: "1"})
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		uc, d = newUseCase(t)
		expectPart(d)

		_, err = uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{
			GarageID: garageID, PartID: partID, Quantity: "1", UnitCost: -1,
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		expectPart(d)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "1.2345").
			Return(usecase.ResolvedQuantity{}, apperror.New(apperror.CodeInvalidArgument, "too many decimals")).Once()

		_, err = uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{GarageID: garageID, PartID: partID, Quantity: "1.2345"})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("storage failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectPart(d)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "1").Return(usecase.ResolvedQuantity{Base: "1"}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{GarageID: garageID, PartID: partID, Quantity: "1"})
		assertAppError(t, err, apperror.CodeInternal)

		unitCopy, enteredCopy := unitID, "1"
		uc, d = newUseCase(t)
		expectPart(d)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "1").Return(usecase.ResolvedQuantity{Base: "1", UnitID: &unitCopy, Entered: &enteredCopy}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, []string{unitID}).Return(nil, errBoom).Once()

		_, err = uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{GarageID: garageID, PartID: partID, Quantity: "1", UnitID: unitID})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		expectPart(d)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "1").Return(usecase.ResolvedQuantity{Base: "1"}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.CreateStockReceipt(t.Context(), usecase.CreateStockReceiptRequest{GarageID: garageID, PartID: partID, Quantity: "1"})
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

func TestInventoryUseCase_IssueStock(t *testing.T) {
	t.Parallel()

	t.Run("deducts stock and publishes stock.issued", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Type == domain.TransactionOut && tx.RefType == domain.RefRepairOrder && *tx.RefID == "ro-1" && tx.Quantity == "2.5"
		})).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventStockIssued, garageID, "", mock.Anything).Return(nil).Once()

		require.NoError(t, uc.IssueStock(t.Context(), garageID, partID, "2.5", "ro-1"))
	})

	t.Run("a deactivated part still issues", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		inactive := samplePart()
		inactive.IsActive = false
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{inactive}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventStockIssued, garageID, "", mock.Anything).Return(nil).Once()

		require.NoError(t, uc.IssueStock(t.Context(), garageID, partID, "1", "ro-1"))
	})

	t.Run("already issued is a no-op", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(domain.ErrAlreadyIssued).Once()

		require.NoError(t, uc.IssueStock(t.Context(), garageID, partID, "2", "ro-1"))
	})

	t.Run("unknown part, bad quantity, storage failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, nil).Once()

		assertAppError(t, uc.IssueStock(t.Context(), garageID, partID, "2", "ro-1"), apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()

		assertAppError(t, uc.IssueStock(t.Context(), garageID, partID, "0", "ro-1"), apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(errBoom).Once()

		assertAppError(t, uc.IssueStock(t.Context(), garageID, partID, "2", "ro-1"), apperror.CodeInternal)
	})

	t.Run("publish failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventStockIssued, garageID, "", mock.Anything).Return(errBoom).Once()

		require.ErrorIs(t, uc.IssueStock(t.Context(), garageID, partID, "2", "ro-1"), errBoom)
	})
}
