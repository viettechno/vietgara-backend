package usecase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	countID     = "c0c0c0c0-0000-0000-0000-000000000001"
	secondCount = "c0c0c0c0-0000-0000-0000-000000000002"
	categoryID  = "ca7e0000-0000-0000-0000-000000000001"
)

// draftCount is a draft count whose lines are the given ones.
func draftCount(lines ...domain.StockCountLine) *domain.StockCount {
	return &domain.StockCount{
		ID: countID, GarageID: garageID, Number: "KK-202610-0001", Name: "Shelf A", Status: domain.StockCountDraft, Lines: lines,
		CreatedBy: new(actorID),
	}
}

// countLineOf is a line of part with the system quantity and, when counted is
// not empty, a counted quantity; the average cost is 50.
func countLineOf(id, part, system, counted string) domain.StockCountLine {
	l := domain.StockCountLine{ID: id, Position: 0, PartID: part, SystemQuantity: system, AverageCost: new("50")}
	if counted != "" {
		l.Counted, l.EnteredCounted = new(counted), new(counted)
	}

	return l
}

// expectCountNames stubs the lookups that fill the derived names.
func expectCountNames(d *deps) {
	d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
	d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, mock.Anything).Return(map[string]string{unitID: "ctn"}, nil).Maybe()
	d.Directory.EXPECT().UserName(mock.Anything, mock.Anything).Return("Lan Nguyen", nil).Maybe()
	d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(map[string]string{supplierA: "Supplier A"}, nil).Maybe()
}

func TestInventoryUseCase_CreateStockCount(t *testing.T) {
	t.Parallel()

	req := usecase.CreateStockCountRequest{GarageID: garageID, ActorID: actorID, Name: "  Shelf A ", Notes: "monthly"}

	t.Run("saves a numbered draft", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "stock_count", "KK-202610-{SEQ:4}").Return(3, nil).Once()
		d.Repo.EXPECT().CreateStockCount(mock.Anything, mock.MatchedBy(func(c *domain.StockCount) bool {
			return c.Number == "KK-202610-0003" && c.Name == "Shelf A" && c.Status == domain.StockCountDraft && *c.CreatedBy == actorID
		})).Return(nil).Once()
		expectCountNames(d)

		c, err := uc.CreateStockCount(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "Lan Nguyen", c.CreatedByName)
		assert.Equal(t, "monthly", c.Notes)
	})

	t.Run("refuses a name that is too long", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)
		bad := req
		bad.Name = strings.Repeat("x", 256)

		_, err := uc.CreateStockCount(t.Context(), bad)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("reports the failing collaborator", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageSettings{}, errBoom).Once()

		_, err := uc.CreateStockCount(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "stock_count", mock.Anything).Return(0, errBoom).Once()

		_, err = uc.CreateStockCount(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "stock_count", mock.Anything).Return(1, nil).Once()
		d.Repo.EXPECT().CreateStockCount(mock.Anything, mock.Anything).Return(errBoom).Once()
		_, err = uc.CreateStockCount(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "stock_count", mock.Anything).Return(1, nil).Once()
		d.Repo.EXPECT().CreateStockCount(mock.Anything, mock.Anything).Return(nil).Once()
		d.Directory.EXPECT().UserName(mock.Anything, mock.Anything).Return("", errBoom).Once()

		_, err = uc.CreateStockCount(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_GetAndListStockCounts(t *testing.T) {
	t.Parallel()

	t.Run("get summarises the lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		c := draftCount(countLineOf("l1", partID, "10", "10"), countLineOf("l2", otherPartID, "10", "12"), countLineOf("l3", "p3", "10", "7"),
			countLineOf("l4", "p4", "10", ""))
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, false).Return(c, nil).Once()
		expectCountNames(d)

		got, err := uc.GetStockCount(t.Context(), garageID, countID)
		require.NoError(t, err)
		assert.Equal(t, domain.Tally{Uncounted: 1, Matched: 1, Over: 1, Short: 1}, got.Tally)
		assert.EqualValues(t, 2*50-3*50, got.TotalDifferenceValue)
		assert.Equal(t, "BEER-330", got.Lines[0].PartSKU)
	})

	t.Run("get reports a missing count and a failing lookup", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, false).Return(nil, domain.ErrStockCountNotFound).Once()
		_, err := uc.GetStockCount(t.Context(), garageID, countID)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, false).Return(draftCount(countLineOf("l1", partID, "1", "")), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		d.Directory.EXPECT().UserName(mock.Anything, mock.Anything).Return("x", nil).Maybe()

		_, err = uc.GetStockCount(t.Context(), garageID, countID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("lists a page", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		listed := draftCount()
		listed.Tally = domain.Tally{Matched: 2}
		d.Repo.EXPECT().ListStockCounts(mock.Anything, garageID, usecase.StockCountFilter{
			Query: "shelf", Status: domain.StockCountDraft, Limit: 10, Offset: 10,
		}).Return([]*domain.StockCount{listed}, 11, nil).Once()
		expectCountNames(d)

		page, err := uc.ListStockCounts(t.Context(), usecase.ListStockCountsRequest{
			PageRequest: usecase.PageRequest{Page: 2, PageSize: 10}, GarageID: garageID, Query: "shelf", Status: domain.StockCountDraft,
		})
		require.NoError(t, err)
		assert.Equal(t, 11, page.Total)
		assert.Equal(t, domain.Tally{Matched: 2}, page.Items[0].Tally, "list pages keep the tally the repository computed")
		assert.Equal(t, "Lan Nguyen", page.Items[0].CreatedByName)

		d.Repo.EXPECT().ListStockCounts(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()
		_, err = uc.ListStockCounts(t.Context(), usecase.ListStockCountsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_UpdateStockCount(t *testing.T) {
	t.Parallel()

	t.Run("renames and annotates a draft", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(), nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.MatchedBy(func(c *domain.StockCount) bool {
			return c.Name == "Shelf B" && c.Notes == "second pass" && *c.UpdatedBy == actorID
		})).Return(nil).Once()
		expectCountNames(d)

		c, err := uc.UpdateStockCount(t.Context(), usecase.UpdateStockCountRequest{
			GarageID: garageID, CountID: countID, ActorID: actorID, Name: new(" Shelf B "), Notes: new("second pass"),
		})
		require.NoError(t, err)
		assert.Equal(t, "Shelf B", c.Name)
	})

	t.Run("refuses a count that is not a draft, a long name and a failing save", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		done := draftCount()
		done.Status = domain.StockCountCompleted
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(done, nil).Once()
		_, err := uc.UpdateStockCount(t.Context(), usecase.UpdateStockCountRequest{GarageID: garageID, CountID: countID})
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(), nil).Once()
		_, err = uc.UpdateStockCount(t.Context(), usecase.UpdateStockCountRequest{
			GarageID: garageID, CountID: countID, Name: new(strings.Repeat("y", 300)),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(), nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.UpdateStockCount(t.Context(), usecase.UpdateStockCountRequest{GarageID: garageID, CountID: countID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

//nolint:funlen // one table of cases
func TestInventoryUseCase_AddStockCountLines(t *testing.T) {
	t.Parallel()

	add := func(partIDs ...string) usecase.AddStockCountLinesRequest {
		return usecase.AddStockCountLinesRequest{GarageID: garageID, CountID: countID, ActorID: actorID, PartIDs: partIDs}
	}

	t.Run("adds picked parts with the system quantity, skipping those already on the count", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).
			Return(draftCount(countLineOf("l1", otherPartID, "3", "")), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).
			Return([]usecase.PartInfo{samplePart(), {ID: otherPartID, IsActive: true}}, nil).Maybe()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).
			Return(map[string]usecase.Balance{partID: {OnHand: "8", AverageCost: "120"}}, nil).Once()
		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, countID, mock.Anything, mock.MatchedBy(func(lines []domain.StockCountLine) bool {
			return len(lines) == 1 && lines[0].PartID == partID && lines[0].SystemQuantity == "8" && *lines[0].AverageCost == "120" &&
				lines[0].Position == 1 && lines[0].Counted == nil
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, mock.Anything).Return(nil, nil).Maybe()
		d.Directory.EXPECT().UserName(mock.Anything, mock.Anything).Return("", nil).Maybe()

		c, err := uc.AddStockCountLines(t.Context(), add(partID, otherPartID, partID))
		require.NoError(t, err)
		assert.Len(t, c.Lines, 2)
		assert.Equal(t, 2, c.Tally.Uncounted)
	})

	t.Run("a part not in stock starts at zero", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).Return(map[string]usecase.Balance{}, nil).Once()
		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, countID, mock.Anything, mock.MatchedBy(func(lines []domain.StockCountLine) bool {
			return lines[0].SystemQuantity == "0" && *lines[0].AverageCost == "0"
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, mock.Anything).Return(nil, nil).Maybe()
		d.Directory.EXPECT().UserName(mock.Anything, mock.Anything).Return("", nil).Maybe()

		_, err := uc.AddStockCountLines(t.Context(), add(partID))
		require.NoError(t, err)
	})

	t.Run("adds the stocked parts of a category, paging through them", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(), nil).Once()
		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, usecase.PartQuery{CategoryID: categoryID, Limit: usecase.MaxPageSize}).
			Return([]usecase.PartInfo{samplePart(), {ID: otherPartID}}, 2, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID, otherPartID}).
			Return(map[string]usecase.Balance{partID: {OnHand: "2"}}, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).
			Return(map[string]usecase.Balance{partID: {OnHand: "2"}}, nil).Once()
		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, countID, mock.Anything, mock.MatchedBy(func(lines []domain.StockCountLine) bool {
			return len(lines) == 1 && lines[0].PartID == partID
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, mock.Anything).Return(nil, nil).Maybe()
		d.Directory.EXPECT().UserName(mock.Anything, mock.Anything).Return("", nil).Maybe()

		req := add()
		req.CategoryID = categoryID

		c, err := uc.AddStockCountLines(t.Context(), req)
		require.NoError(t, err)
		assert.Len(t, c.Lines, 1)
	})

	t.Run("refuses nothing to add, unknown and inactive parts, and other states", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(), nil).Times(3)

		_, err := uc.AddStockCountLines(t.Context(), add())
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, nil).Once()
		_, err = uc.AddStockCountLines(t.Context(), add(partID))
		assertAppError(t, err, apperror.CodeNotFound)

		inactive := samplePart()
		inactive.IsActive = false
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{inactive}, nil).Once()
		_, err = uc.AddStockCountLines(t.Context(), add(partID))
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		done := draftCount()
		done.Status = domain.StockCountCancelled
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(done, nil).Once()

		_, err = uc.AddStockCountLines(t.Context(), add(partID))
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("reports failing lookups and writes", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(), nil).Times(4)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Times(2)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err := uc.AddStockCountLines(t.Context(), add(partID))
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, countID, mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.AddStockCountLines(t.Context(), add(partID))
		assertAppError(t, err, apperror.CodeInternal)

		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		req := add()
		req.CategoryID = categoryID
		_, err = uc.AddStockCountLines(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		d.Parts.EXPECT().SearchParts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, 1, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.AddStockCountLines(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

//nolint:funlen // one table of cases
func TestInventoryUseCase_UpdateStockCountLine(t *testing.T) {
	t.Parallel()

	req := func(unit, entered string) usecase.UpdateStockCountLineRequest {
		return usecase.UpdateStockCountLineRequest{
			GarageID: garageID, CountID: countID, LineID: "l1", ActorID: actorID, UnitID: unit, EnteredCounted: entered,
		}
	}

	setup := func(t *testing.T) (*usecase.InventoryUseCase, *deps) {
		t.Helper()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(countLineOf("l1", partID, "1", "")), nil).Once()
		expectCountNames(d)

		return uc, d
	}

	expectBalance := func(d *deps) {
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).
			Return(map[string]usecase.Balance{partID: {OnHand: "9", AverageCost: "60"}}, nil).Once()
	}

	t.Run("types a quantity in the base unit and snapshots the system quantity", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "8").Return(usecase.ResolvedQuantity{Base: "8"}, nil).Once()
		expectBalance(d)
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.MatchedBy(func(l *domain.StockCountLine) bool {
			return *l.Counted == "8" && *l.EnteredCounted == "8" && l.UnitID == nil && l.SystemQuantity == "9" && *l.AverageCost == "60" &&
				l.CountedAt != nil
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()

		c, err := uc.UpdateStockCountLine(t.Context(), req("", "8"))
		require.NoError(t, err)
		assert.Equal(t, domain.CountShort, c.Lines[0].Result())
		assert.Equal(t, now, *c.Lines[0].CountedAt)
	})

	t.Run("converts a quantity typed in a packaging unit", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "2").
			Return(usecase.ResolvedQuantity{Base: "24", UnitID: new(unitID), Entered: new("2")}, nil).Once()
		expectBalance(d)
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()

		c, err := uc.UpdateStockCountLine(t.Context(), req(unitID, "2"))
		require.NoError(t, err)
		assert.Equal(t, "24", *c.Lines[0].Counted)
		assert.Equal(t, "2", *c.Lines[0].EnteredCounted)
		assert.Equal(t, "ctn", c.Lines[0].UnitAbbreviation)
	})

	t.Run("zero is a count of none in any unit", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		expectBalance(d)
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.MatchedBy(func(l *domain.StockCountLine) bool {
			return *l.Counted == "0" && l.UnitID == nil
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()

		c, err := uc.UpdateStockCountLine(t.Context(), req(unitID, "0"))
		require.NoError(t, err)
		assert.Equal(t, domain.CountShort, c.Lines[0].Result())
	})

	t.Run("an empty quantity marks the line uncounted again", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(countLineOf("l1", partID, "1", "5")), nil).Once()
		expectCountNames(d)
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.MatchedBy(func(l *domain.StockCountLine) bool {
			return l.Counted == nil && l.EnteredCounted == nil && l.CountedAt == nil && l.SystemQuantity == "1"
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()

		c, err := uc.UpdateStockCountLine(t.Context(), req("", "  "))
		require.NoError(t, err)
		assert.Equal(t, domain.CountUncounted, c.Lines[0].Result())
	})

	t.Run("refuses bad input and reports failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(countLineOf("l1", partID, "1", "")), nil).Times(5)
		expectCountNames(d)

		_, err := uc.UpdateStockCountLine(t.Context(), req("", "1.2345"))
		assertAppError(t, err, apperror.CodeInvalidArgument)

		missing := req("", "1")
		missing.LineID = "nope"
		_, err = uc.UpdateStockCountLine(t.Context(), missing)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "1").Return(usecase.ResolvedQuantity{}, errBoom).Once()

		_, err = uc.UpdateStockCountLine(t.Context(), req("", "1"))
		assertAppError(t, err, apperror.CodeInternal)

		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "1").Return(usecase.ResolvedQuantity{Base: "1"}, nil).Twice()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.UpdateStockCountLine(t.Context(), req("", "1"))
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.Anything).Return(errBoom).Once()

		_, err = uc.UpdateStockCountLine(t.Context(), req("", "1"))
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_DeleteStockCountLine(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).
		Return(draftCount(countLineOf("l1", partID, "1", ""), countLineOf("l2", otherPartID, "1", "")), nil).Times(4)
	d.Repo.EXPECT().DeleteStockCountLine(mock.Anything, garageID, countID, "l1").Return(nil).Once()
	d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()
	expectCountNames(d)

	c, err := uc.DeleteStockCountLine(t.Context(), garageID, countID, "l1", actorID)
	require.NoError(t, err)
	require.Len(t, c.Lines, 1)
	assert.Equal(t, "l2", c.Lines[0].ID)

	_, err = uc.DeleteStockCountLine(t.Context(), garageID, countID, "nope", actorID)
	assertAppError(t, err, apperror.CodeNotFound)

	d.Repo.EXPECT().DeleteStockCountLine(mock.Anything, garageID, countID, "l2").Return(errBoom).Once()
	_, err = uc.DeleteStockCountLine(t.Context(), garageID, countID, "l2", actorID)
	assertAppError(t, err, apperror.CodeInternal)

	d.Repo.EXPECT().DeleteStockCountLine(mock.Anything, garageID, countID, "l2").Return(nil).Once()
	d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(errBoom).Once()

	_, err = uc.DeleteStockCountLine(t.Context(), garageID, countID, "l2", actorID)
	assertAppError(t, err, apperror.CodeInternal)

	d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, "gone", true).Return(nil, domain.ErrStockCountNotFound).Once()
	_, err = uc.DeleteStockCountLine(t.Context(), garageID, "gone", "l1", actorID)
	assertAppError(t, err, apperror.CodeNotFound)
}

//nolint:funlen // one table of cases
func TestInventoryUseCase_MergeStockCounts(t *testing.T) {
	t.Parallel()

	t0 := now.Add(-time.Hour)
	older := countLineOf("a1", partID, "10", "4")
	older.CountedAt = &t0
	later := countLineOf("b1", partID, "12", "6")
	later.CountedAt = &now

	req := usecase.MergeStockCountsRequest{GarageID: garageID, ActorID: actorID, CountIDs: []string{countID, secondCount}}

	second := func(lines ...domain.StockCountLine) *domain.StockCount {
		c := draftCount(lines...)
		c.ID, c.Number, c.Notes = secondCount, "KK-202610-0002", "tidy"

		return c
	}

	t.Run("adds up a part counted twice and moves the rest", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		moved := countLineOf("b2", "p3", "2", "1")
		uncounted := countLineOf("b3", "p4", "2", "")

		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(older), nil).Once()
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, secondCount, true).Return(second(later, moved, uncounted), nil).Once()
		d.Repo.EXPECT().ClearStockCountLines(mock.Anything, garageID, secondCount).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.MatchedBy(func(c *domain.StockCount) bool {
			return c.ID == secondCount && c.Status == domain.StockCountCancelled && c.CancelledAt != nil &&
				c.Notes == "tidy\nMerged into KK-202610-0001"
		})).Return(nil).Once()
		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, countID, mock.Anything, mock.MatchedBy(func(lines []domain.StockCountLine) bool {
			return len(lines) == 2 && lines[0].Position == 1 && lines[1].Position == 2
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.MatchedBy(func(l *domain.StockCountLine) bool {
			return l.PartID == partID && *l.Counted == "10" && l.UnitID == nil && l.SystemQuantity == "12"
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.MatchedBy(func(c *domain.StockCount) bool { return c.ID == countID })).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
		expectCountNames(d)

		c, err := uc.MergeStockCounts(t.Context(), req)
		require.NoError(t, err)
		assert.Len(t, c.Lines, 3)
		assert.Equal(t, domain.Tally{Uncounted: 1, Over: 0, Short: 2}, c.Tally)
	})

	t.Run("a counted line replaces an uncounted one of the same part", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(countLineOf("a1", partID, "10", "")), nil).Once()
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, secondCount, true).
			Return(second(countLineOf("b1", partID, "11", "5"), countLineOf("b2", partID+"x", "1", "")), nil).Once()
		d.Repo.EXPECT().ClearStockCountLines(mock.Anything, garageID, secondCount).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Twice()
		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, countID, mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.MatchedBy(func(l *domain.StockCountLine) bool {
			return *l.Counted == "5" && l.SystemQuantity == "11"
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
		expectCountNames(d)

		_, err := uc.MergeStockCounts(t.Context(), req)
		require.NoError(t, err)
	})

	t.Run("an uncounted line adds nothing, and the later snapshot wins", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(older), nil).Once()
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, secondCount, true).Return(second(countLineOf("b1", partID, "99", "")), nil).Once()
		d.Repo.EXPECT().ClearStockCountLines(mock.Anything, garageID, secondCount).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Twice()
		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, countID, mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.MatchedBy(func(l *domain.StockCountLine) bool {
			return *l.Counted == "4" && l.SystemQuantity == "10"
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
		expectCountNames(d)

		_, err := uc.MergeStockCounts(t.Context(), req)
		require.NoError(t, err)

		// Counted on both: the later snapshot of the system quantity is kept.
		first := countLineOf("a1", partID, "10", "4")
		first.CountedAt = &t0
		recent := countLineOf("b1", partID, "12", "6")
		recent.CountedAt = &now

		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(first), nil).Once()
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, secondCount, true).Return(second(recent), nil).Once()
		d.Repo.EXPECT().ClearStockCountLines(mock.Anything, garageID, secondCount).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Twice()
		d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, countID, mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.MatchedBy(func(l *domain.StockCountLine) bool {
			return *l.Counted == "10" && l.SystemQuantity == "12" && l.CountedAt.Equal(now)
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		_, err = uc.MergeStockCounts(t.Context(), req)
		require.NoError(t, err)
	})

	t.Run("refuses fewer than two counts and counts that are not drafts", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		_, err := uc.MergeStockCounts(t.Context(), usecase.MergeStockCountsRequest{GarageID: garageID, CountIDs: []string{countID, countID}})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		done := second()
		done.Status = domain.StockCountCompleted

		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(), nil).Once()
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, secondCount, true).Return(done, nil).Once()

		_, err = uc.MergeStockCounts(t.Context(), req)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("reports failing writes", func(t *testing.T) {
		t.Parallel()

		for step := range 6 {
			uc, d := newUseCase(t)
			d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(older), nil).Once()
			d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, secondCount, true).Return(second(later, countLineOf("b2", "p3", "2", "1")), nil).Once()
			expectCountNames(d)
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Maybe()

			fail := func(i int) error {
				if i == step {
					return errBoom
				}

				return nil
			}

			d.Repo.EXPECT().ClearStockCountLines(mock.Anything, garageID, secondCount).Return(fail(0)).Maybe()
			d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.MatchedBy(func(c *domain.StockCount) bool { return c.ID == secondCount })).Return(fail(1)).Maybe()
			d.Repo.EXPECT().InsertStockCountLines(mock.Anything, garageID, countID, mock.Anything, mock.Anything).Return(fail(2)).Maybe()
			d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.Anything).Return(fail(3)).Maybe()
			d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.MatchedBy(func(c *domain.StockCount) bool { return c.ID == countID })).Return(fail(4)).Maybe()

			_, err := uc.MergeStockCounts(t.Context(), req)
			assertAppError(t, err, apperror.CodeInternal)
		}
	})
}

// expectAdjustment stubs the movement of a count line.
func expectAdjustment(d *deps, part string, kind domain.TransactionType, quantity string, cost *string) {
	d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
		return tx.PartID == part && tx.Type == kind && tx.Quantity == quantity && tx.RefType == domain.RefStockCount &&
			*tx.RefID == countID && assert.ObjectsAreEqual(cost, tx.UnitCost)
	})).Return(nil).Once()
}

func TestInventoryUseCase_CompleteStockCount(t *testing.T) {
	t.Parallel()

	req := usecase.CompleteStockCountRequest{GarageID: garageID, CountID: countID, ActorID: actorID}

	t.Run("posts the movement that makes stock equal the count", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(
			countLineOf("l1", partID, "1", "7"), countLineOf("l2", otherPartID, "1", "2"), countLineOf("l3", "p3", "1", "1"),
		), nil).Once()
		d.Repo.EXPECT().LockBalances(mock.Anything, garageID, []string{partID, otherPartID, "p3"}).Return(nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID, otherPartID, "p3"}).Return(map[string]usecase.Balance{
			partID: {OnHand: "4", AverageCost: "100"}, otherPartID: {OnHand: "5", AverageCost: "10"}, "p3": {OnHand: "1", AverageCost: "5"},
		}, nil).Once()
		expectAdjustment(d, partID, domain.TransactionIn, "3", nil)
		expectAdjustment(d, otherPartID, domain.TransactionOut, "3", nil)
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.MatchedBy(func(l *domain.StockCountLine) bool {
			return l.SystemQuantity != "1" || l.PartID == "p3"
		})).Return(nil).Times(3)
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.MatchedBy(func(c *domain.StockCount) bool {
			return c.Status == domain.StockCountCompleted && c.CompletedAt != nil && *c.CompletedBy == actorID
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
		expectCountNames(d)

		c, err := uc.CompleteStockCount(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, domain.StockCountCompleted, c.Status)
		assert.Equal(t, "4", c.Lines[0].SystemQuantity, "the final snapshot replaces the earlier one")
		assert.Equal(t, domain.Tally{Matched: 1, Over: 1, Short: 1}, c.Tally)
		assert.EqualValues(t, 3*100-3*10, c.TotalDifferenceValue)
	})

	t.Run("refuses uncounted lines unless asked to skip them", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		lines := func() *domain.StockCount {
			return draftCount(countLineOf("l1", partID, "4", "4"), countLineOf("l2", otherPartID, "1", ""))
		}
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(lines(), nil).Twice()

		_, err := uc.CompleteStockCount(t.Context(), req)
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		skip := req
		skip.SkipUncounted = true

		d.Repo.EXPECT().DeleteStockCountLine(mock.Anything, garageID, countID, "l2").Return(nil).Once()
		d.Repo.EXPECT().LockBalances(mock.Anything, garageID, []string{partID}).Return(nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).Return(map[string]usecase.Balance{partID: {OnHand: "4"}}, nil).Once()
		d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
		expectCountNames(d)

		c, err := uc.CompleteStockCount(t.Context(), skip)
		require.NoError(t, err)
		assert.Len(t, c.Lines, 1, "no movement for a matching line")

		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(lines(), nil).Once()
		d.Repo.EXPECT().DeleteStockCountLine(mock.Anything, garageID, countID, "l2").Return(errBoom).Once()

		_, err = uc.CompleteStockCount(t.Context(), skip)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("refuses a count without counted lines and a count that is not a draft", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(), nil).Once()

		_, err := uc.CompleteStockCount(t.Context(), req)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		done := draftCount()
		done.Status = domain.StockCountCompleted
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(done, nil).Once()

		_, err = uc.CompleteStockCount(t.Context(), req)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("reports failing writes", func(t *testing.T) {
		t.Parallel()

		for step := range 7 {
			uc, d := newUseCase(t)
			d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(countLineOf("l1", partID, "1", "7")), nil).Once()
			expectCountNames(d)
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Maybe()

			fail := func(i int) error {
				if i == step {
					return errBoom
				}

				return nil
			}

			d.Repo.EXPECT().LockBalances(mock.Anything, garageID, mock.Anything).Return(fail(0)).Maybe()
			d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(map[string]usecase.Balance{partID: {OnHand: "1"}}, fail(1)).Maybe()
			d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(fail(2)).Maybe()
			d.Repo.EXPECT().SaveStockCountLine(mock.Anything, garageID, countID, mock.Anything).Return(fail(3)).Maybe()
			d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(fail(4)).Maybe()

			_, err := uc.CompleteStockCount(t.Context(), req)
			assertAppError(t, err, apperror.CodeInternal)
		}
	})
}

func TestInventoryUseCase_CancelStockCount(t *testing.T) {
	t.Parallel()

	completed := func() *domain.StockCount {
		c := draftCount(
			countLineOf("l1", partID, "4", "7"), countLineOf("l2", otherPartID, "5", "2"), countLineOf("l3", "p3", "1", "1"),
			countLineOf("l4", "p4", "3", "0"),
		)
		c.Lines[3].AverageCost = new("0")
		c.Status = domain.StockCountCompleted

		return c
	}

	t.Run("closing a draft posts nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(draftCount(countLineOf("l1", partID, "4", "")), nil).Once()
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.MatchedBy(func(c *domain.StockCount) bool {
			return c.Status == domain.StockCountCancelled && c.CancelledAt != nil
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
		expectCountNames(d)

		c, err := uc.CancelStockCount(t.Context(), garageID, countID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.StockCountCancelled, c.Status)
	})

	t.Run("a completed count is reversed line by line", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(completed(), nil).Once()
		d.Repo.EXPECT().LockBalances(mock.Anything, garageID, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(map[string]usecase.Balance{partID: {OnHand: "7"}}, nil).Once()
		expectAdjustment(d, partID, domain.TransactionOut, "3", nil)
		expectAdjustment(d, otherPartID, domain.TransactionIn, "3", new("50"))
		expectAdjustment(d, "p4", domain.TransactionIn, "3", nil)
		d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
		expectCountNames(d)

		c, err := uc.CancelStockCount(t.Context(), garageID, countID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.StockCountCancelled, c.Status)
	})

	t.Run("refuses when the added stock has been used, and a second cancel", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(completed(), nil).Once()
		d.Repo.EXPECT().LockBalances(mock.Anything, garageID, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(map[string]usecase.Balance{partID: {OnHand: "2"}}, nil).Once()
		_, err := uc.CancelStockCount(t.Context(), garageID, countID, actorID)
		require.Error(t, err)
		assert.Equal(t, usecase.ReasonInsufficientStockToReverse, mustReason(t, err))

		gone := draftCount()
		gone.Status = domain.StockCountCancelled
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(gone, nil).Once()
		_, err = uc.CancelStockCount(t.Context(), garageID, countID, actorID)
		assert.Equal(t, usecase.ReasonStockCountAlreadyCancelled, mustReason(t, err))
	})

	t.Run("reports failing lookups and writes", func(t *testing.T) {
		t.Parallel()

		for step := range 6 {
			uc, d := newUseCase(t)
			d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(completed(), nil).Once()
			expectCountNames(d)
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Maybe()

			fail := func(i int) error {
				if i == step {
					return errBoom
				}

				return nil
			}

			d.Repo.EXPECT().LockBalances(mock.Anything, garageID, mock.Anything).Return(fail(0)).Maybe()
			d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(map[string]usecase.Balance{partID: {OnHand: "7"}}, fail(1)).Maybe()
			d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(fail(2)).Maybe()
			d.Repo.EXPECT().SaveStockCount(mock.Anything, mock.Anything).Return(fail(3)).Maybe()

			_, err := uc.CancelStockCount(t.Context(), garageID, countID, actorID)
			assertAppError(t, err, apperror.CodeInternal)
		}

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockCount(mock.Anything, garageID, countID, true).Return(nil, domain.ErrStockCountNotFound).Once()
		_, err := uc.CancelStockCount(t.Context(), garageID, countID, actorID)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

// mustReason returns the reason of an application error.
func mustReason(t *testing.T, err error) string {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)

	return appErr.Reason
}
