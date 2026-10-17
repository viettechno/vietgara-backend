package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// endOfDay matches midnight at the start of 2026-10-16 in Ho Chi Minh City.
func endOfDay(upTo time.Time) bool {
	return upTo.Equal(time.Date(2026, 10, 16, 0, 0, 0, 0, time.FixedZone("ICT", 7*3600)))
}

func TestInventoryUseCase_PurchasesReport(t *testing.T) {
	t.Parallel()

	t.Run("names the parts and suppliers and totals the range", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().PurchasesReport(mock.Anything, garageID, usecase.PurchaseFilter{SupplierID: supplierA, From: "2026-10-01", To: "2026-11-01"}).
			Return([]domain.PurchaseLine{
				{PartID: partID, SupplierID: supplierA, Quantity: "10", UnitCost: 100, Amount: 1000},
				{PartID: partID, SupplierID: supplierA, Quantity: "5", UnitCost: 110, Amount: 550},
			}, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, []string{supplierA}).Return(supplierNames(supplierA), nil).Once()

		report, err := uc.PurchasesReport(t.Context(), usecase.PurchasesReportRequest{
			GarageID: garageID, SupplierID: supplierA, From: "2026-10-01", To: "2026-11-01",
		})
		require.NoError(t, err)
		assert.Equal(t, int64(1550), report.TotalAmount)
		assert.Equal(t, "Beer 330 ml", report.Lines[1].PartName)
		assert.Equal(t, "Supplier 1", report.Lines[0].SupplierName)
	})

	t.Run("bad ranges and failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)

		for _, r := range []usecase.PurchasesReportRequest{
			{GarageID: garageID, From: "yesterday"},
			{GarageID: garageID, To: "tomorrow"},
			{GarageID: garageID, From: "2026-10-02", To: "2026-10-02"},
			{GarageID: garageID, From: "2026-10-03", To: "2026-10-02"},
		} {
			_, err := uc.PurchasesReport(t.Context(), r)
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}

		d.Repo.EXPECT().PurchasesReport(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		_, err := uc.PurchasesReport(t.Context(), usecase.PurchasesReportRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_StockValuation(t *testing.T) {
	t.Parallel()

	oil := samplePart()
	oil.CategoryID, oil.CategoryName = "cat-1", "Oils"
	filter := samplePart()
	filter.ID, filter.Name, filter.CategoryID, filter.CategoryName = otherPartID, "Air filter", "cat-2", "Filters"
	loose := samplePart()
	loose.ID, loose.Name, loose.CategoryID, loose.CategoryName = "33333333-3333-3333-3333-333333333339", "Loose bolt", "", ""

	t.Run("values every part with stock, per category, at the end of the day", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		// 2026-10-15 in Ho Chi Minh City ends at 2026-10-15 17:00 UTC.
		d.Repo.EXPECT().StockValuation(mock.Anything, garageID, mock.MatchedBy(endOfDay)).
			Return([]usecase.ValuationRow{
				{PartID: partID, Quantity: "10", AverageCost: "100.5"},
				{PartID: otherPartID, Quantity: "3", AverageCost: "2000"},
				{PartID: loose.ID, Quantity: "0", AverageCost: "10"},
				{PartID: "33333333-3333-3333-3333-333333333338", Quantity: "2", AverageCost: "1.25"},
			}, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{oil, filter, loose}, nil).Once()

		report, err := uc.StockValuation(t.Context(), garageID, "2026-10-15")
		require.NoError(t, err)
		assert.Equal(t, "2026-10-15", report.At)
		assert.Equal(t, int64(1005+6000+3), report.TotalValue, "10 x 100.5 + 3 x 2000 + 2 x 1.25, each rounded to whole dong")
		require.Len(t, report.Lines, 3, "a part at zero is left out")
		assert.Empty(t, report.Lines[0].CategoryName, "uncategorised parts sort first")
		assert.Equal(t, "Filters", report.Lines[1].CategoryName)
		assert.Equal(t, int64(101), report.Lines[2].AverageCost, "100.5 rounds half up")
		require.Len(t, report.Categories, 3)
		assert.Equal(t, int64(1005), report.Categories[2].Value)
	})

	t.Run("an empty date is today in the garage's time zone", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Repo.EXPECT().StockValuation(mock.Anything, garageID, mock.MatchedBy(endOfDay)).Return(nil, nil).Once()

		report, err := uc.StockValuation(t.Context(), garageID, "")
		require.NoError(t, err)
		assert.Equal(t, "2026-10-15", report.At)
		assert.Empty(t, report.Lines)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Times(3)

		_, err := uc.StockValuation(t.Context(), garageID, "soon")
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Repo.EXPECT().StockValuation(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		_, err = uc.StockValuation(t.Context(), garageID, "")
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().StockValuation(mock.Anything, garageID, mock.Anything).Return([]usecase.ValuationRow{{PartID: partID, Quantity: "1"}}, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		_, err = uc.StockValuation(t.Context(), garageID, "")
		assertAppError(t, err, apperror.CodeInternal)

		d.Garages.EXPECT().Get(mock.Anything, garageID).Unset()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageSettings{}, errBoom).Once()
		_, err = uc.StockValuation(t.Context(), garageID, "")
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_PartSuppliers(t *testing.T) {
	t.Parallel()

	t.Run("get names the suppliers", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().PartSuppliers(mock.Anything, garageID, partID).
			Return([]domain.PartSupplier{{SupplierID: supplierA, IsDefault: true, LastUnitCost: new("6250")}}, nil).Once()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, []string{supplierA}).Return(supplierNames(supplierA), nil).Once()

		list, err := uc.GetPartSuppliers(t.Context(), garageID, partID)
		require.NoError(t, err)
		assert.Equal(t, "Supplier 1", list[0].SupplierName)
	})

	t.Run("put replaces the list", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		in := []domain.PartSupplier{{SupplierID: supplierA, SupplierSKU: "A-1", IsDefault: true}, {SupplierID: supplierB}}

		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA, supplierB), nil).Twice()
		d.Repo.EXPECT().ReplacePartSuppliers(mock.Anything, garageID, partID, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().PartSuppliers(mock.Anything, garageID, partID).Return(in, nil).Once()

		list, err := uc.PutPartSuppliers(t.Context(), garageID, partID, in)
		require.NoError(t, err)
		assert.Len(t, list, 2)
	})

	t.Run("rejections and failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, nil).Once()
		_, err := uc.GetPartSuppliers(t.Context(), garageID, partID)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Times(5)

		for _, list := range [][]domain.PartSupplier{
			{{SupplierID: supplierA}, {SupplierID: supplierA}},
			{{SupplierID: supplierA, IsDefault: true}, {SupplierID: supplierB, IsDefault: true}},
		} {
			_, err = uc.PutPartSuppliers(t.Context(), garageID, partID, list)
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}

		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(), nil).Once()
		_, err = uc.PutPartSuppliers(t.Context(), garageID, partID, []domain.PartSupplier{{SupplierID: supplierA}})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA), nil).Once()
		d.Repo.EXPECT().ReplacePartSuppliers(mock.Anything, garageID, partID, mock.Anything).Return(errBoom).Once()
		_, err = uc.PutPartSuppliers(t.Context(), garageID, partID, []domain.PartSupplier{{SupplierID: supplierA}})
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().PartSuppliers(mock.Anything, garageID, partID).Return(nil, errBoom).Once()
		_, err = uc.GetPartSuppliers(t.Context(), garageID, partID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_GetStockLevel_Reservation(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
	d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).
		Return(map[string]usecase.Balance{partID: {OnHand: "12", Reserved: "3.5", AverageCost: "100.25"}}, nil).Once()

	level, err := uc.GetStockLevel(t.Context(), garageID, partID)
	require.NoError(t, err)
	assert.Equal(t, "8.5", level.QuantityAvailable, "on hand minus reserved")
	assert.Equal(t, "3.5", level.QuantityReserved)
	assert.Equal(t, "100.25", *level.AverageCost)
	assert.Equal(t, "1203", *level.StockValue)
}
