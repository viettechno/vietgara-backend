package usecase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const secondPart = "55555555-5555-5555-5555-555555555555"

// expectImportedCount stubs the header of an imported count.
func expectImportedCount(d *deps) {
	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
	d.Sequences.EXPECT().Next(mock.Anything, garageID, "stock_count", "KK-202610-{SEQ:4}").Return(1, nil).Once()
	d.Repo.EXPECT().CreateStockCount(mock.Anything, mock.Anything).Return(nil).Once()
	expectCountNames(d)
}

//nolint:funlen // one table of import cases
func TestInventoryUseCase_ImportStockCount(t *testing.T) {
	t.Parallel()

	req := usecase.ImportStockCountRequest{
		GarageID: garageID, ActorID: actorID, Name: "Shelf A", Notes: "from Excel",
		Lines: []usecase.CountedPart{
			{PartID: partID, EnteredCounted: "7"},
			{PartID: secondPart, UnitID: unitID, EnteredCounted: "2"},
			{PartID: partID, EnteredCounted: "9"},
		},
	}

	two := samplePart()
	two.ID = secondPart

	t.Run("saves one counted line per part, converting only packaging units", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID, secondPart}).
			Return([]usecase.PartInfo{samplePart(), two}, nil).Once()
		expectImportedCount(d)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID, secondPart}).Return(map[string]usecase.Balance{
			partID: {OnHand: "10", AverageCost: "50"},
		}, nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, secondPart, unitID, "2").
			Return(usecase.ResolvedQuantity{Base: "24", UnitID: new(unitID), Entered: new("2")}, nil).Once()

		var saved []domain.StockCountLine

		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, mock.Anything, mock.Anything, mock.Anything).
			Run(func(_ context.Context, _, _ string, _ *string, lines []domain.StockCountLine) { saved = lines }).Return(nil).Once()

		c, err := uc.ImportStockCount(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "KK-202610-0001", c.Number)

		require.Len(t, saved, 2, "a part named twice is counted once, the first count wins")
		assert.Equal(t, partID, saved[0].PartID)
		assert.Equal(t, "7", *saved[0].Counted)
		assert.Equal(t, "10", saved[0].SystemQuantity, "the stock on hand when counted")
		assert.Equal(t, "50", *saved[0].AverageCost)
		assert.Nil(t, saved[0].UnitID, "a base-unit count is not converted")
		assert.Equal(t, now, *saved[0].CountedAt)

		assert.Equal(t, secondPart, saved[1].PartID)
		assert.Equal(t, "24", *saved[1].Counted, "2 cartons in the base unit")
		assert.Equal(t, "2", *saved[1].EnteredCounted)
		assert.Equal(t, unitID, *saved[1].UnitID)
		assert.Equal(t, "0", saved[1].SystemQuantity, "a part without a balance has none on hand")
		assert.Equal(t, 1, saved[1].Position)
	})

	t.Run("zero is a real count; an empty quantity is left uncounted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart(), two}, nil).Once()
		expectImportedCount(d)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()

		var saved []domain.StockCountLine

		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, mock.Anything, mock.Anything, mock.Anything).
			Run(func(_ context.Context, _, _ string, _ *string, lines []domain.StockCountLine) { saved = lines }).Return(nil).Once()

		_, err := uc.ImportStockCount(t.Context(), usecase.ImportStockCountRequest{
			GarageID: garageID, ActorID: actorID,
			Lines: []usecase.CountedPart{{PartID: partID, EnteredCounted: "0"}, {PartID: secondPart}},
		})
		require.NoError(t, err)

		require.Len(t, saved, 2)
		assert.Equal(t, "0", *saved[0].Counted)
		assert.Nil(t, saved[1].Counted)
		assert.Nil(t, saved[1].CountedAt)
	})

	t.Run("refuses what cannot be counted", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := uc.ImportStockCount(t.Context(), usecase.ImportStockCountRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()

		_, err = uc.ImportStockCount(t.Context(), usecase.ImportStockCountRequest{
			GarageID: garageID, Lines: []usecase.CountedPart{{PartID: partID, EnteredCounted: "1"}, {PartID: secondPart, EnteredCounted: "1"}},
		})
		assertAppError(t, err, apperror.CodeNotFound)

		inactive := samplePart()
		inactive.IsActive = false

		uc, d = newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{inactive}, nil).Once()

		_, err = uc.ImportStockCount(t.Context(), usecase.ImportStockCountRequest{
			GarageID: garageID, Lines: []usecase.CountedPart{{PartID: partID, EnteredCounted: "1"}},
		})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("passes failures on", func(t *testing.T) {
		t.Parallel()

		one := usecase.ImportStockCountRequest{
			GarageID: garageID, ActorID: actorID, Lines: []usecase.CountedPart{{PartID: partID, UnitID: unitID, EnteredCounted: "2"}},
		}

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err := uc.ImportStockCount(t.Context(), one)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageSettings{}, errBoom).Once()

		_, err = uc.ImportStockCount(t.Context(), one)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		expectImportedCount(d)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.ImportStockCount(t.Context(), one)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		expectImportedCount(d)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "2").Return(usecase.ResolvedQuantity{}, errBoom).Once()

		_, err = uc.ImportStockCount(t.Context(), one)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		expectImportedCount(d)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
		_, err = uc.ImportStockCount(t.Context(), usecase.ImportStockCountRequest{
			GarageID: garageID, Lines: []usecase.CountedPart{{PartID: partID, EnteredCounted: "-1"}},
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		expectImportedCount(d)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, mock.Anything, mock.Anything, mock.Anything).Return(errBoom).Once()
		_, err = uc.ImportStockCount(t.Context(), usecase.ImportStockCountRequest{
			GarageID: garageID, Lines: []usecase.CountedPart{{PartID: partID, EnteredCounted: "1"}},
		})
		assertAppError(t, err, apperror.CodeInternal)
	})
}
