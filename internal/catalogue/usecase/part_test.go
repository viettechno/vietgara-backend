package usecase_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

//nolint:funlen // one table of create cases
func TestUseCase_CreatePart(t *testing.T) {
	t.Parallel()

	valid := func() usecase.PartFields {
		return usecase.PartFields{
			SKU: new(" BEER-330 "), Name: new("Beer 330 ml"), BaseUnitID: new(unitID), SalePrice: new(int64(15_000)),
			Brand: new("Tiger"), CategoryID: new(catID), ReorderThreshold: new("48.50"),
		}
	}

	t.Run("creates and returns the stored part", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, catID).Return(&domain.Category{ID: catID, Name: "Beverages", IsActive: true}, nil).Once()
		d.Repo.EXPECT().CreatePart(mock.Anything, mock.MatchedBy(func(p *domain.Part) bool {
			return p.SKU == "BEER-330" && p.BaseUnitID == unitID && p.SalePrice == 15_000 && *p.CategoryID == catID &&
				p.ReorderThreshold == "48.5" && p.IsActive && *p.CreatedBy == actorID
		})).Return(nil).Once()
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, mock.Anything).Return(samplePart(), nil).Once()

		part, err := uc.CreatePart(t.Context(), usecase.CreatePartRequest{GarageID: garageID, ActorID: actorID, Fields: valid()})
		require.NoError(t, err)
		assert.Equal(t, "BEER-330", part.SKU)
	})

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().CreatePart(mock.Anything, mock.MatchedBy(func(p *domain.Part) bool {
			return p.SalePrice == 0 && p.CategoryID == nil && p.Brand == "" && p.ReorderThreshold == "0"
		})).Return(nil).Once()
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, mock.Anything).Return(samplePart(), nil).Once()

		_, err := uc.CreatePart(t.Context(), usecase.CreatePartRequest{GarageID: garageID, Fields: usecase.PartFields{
			SKU: new("S"), Name: new("N"), BaseUnitID: new(unitID),
		}})
		require.NoError(t, err)
	})

	t.Run("required fields", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		for _, f := range []usecase.PartFields{
			{Name: new("N"), BaseUnitID: new(unitID)},
			{SKU: new("S"), BaseUnitID: new(unitID)},
			{SKU: new("S"), Name: new("N")},
			{SKU: new("S"), Name: new("N"), BaseUnitID: new("")},
			{SKU: new(" "), Name: new("N"), BaseUnitID: new(unitID)},
			{SKU: new("S"), Name: new(" "), BaseUnitID: new(unitID)},
			{SKU: new(strings.Repeat("x", 51)), Name: new("N"), BaseUnitID: new(unitID)},
			{SKU: new("S"), Name: new(strings.Repeat("x", 256)), BaseUnitID: new(unitID)},
		} {
			_, err := uc.CreatePart(t.Context(), usecase.CreatePartRequest{GarageID: garageID, Fields: f})
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}
	})

	t.Run("invalid values", func(t *testing.T) {
		t.Parallel()

		for name, mutate := range map[string]func(f *usecase.PartFields){
			"negative price": func(f *usecase.PartFields) { f.SalePrice = new(int64(-1)) },
			"brand too long": func(f *usecase.PartFields) { f.Brand = new(strings.Repeat("x", 101)) },
			"bad threshold":  func(f *usecase.PartFields) { f.ReorderThreshold = new("-1") },
		} {
			uc, d := newUseCase(t)
			d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Maybe()
			d.Repo.EXPECT().FindCategory(mock.Anything, garageID, catID).Return(&domain.Category{ID: catID, IsActive: true}, nil).Maybe()

			f := valid()
			mutate(&f)

			_, err := uc.CreatePart(t.Context(), usecase.CreatePartRequest{GarageID: garageID, Fields: f})
			assertAppError(t, err, apperror.CodeInvalidArgument)

			_ = name
		}
	})

	t.Run("unit and category must exist and be active", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(nil, domain.ErrUnitNotFound).Once()

		_, err := uc.CreatePart(t.Context(), usecase.CreatePartRequest{GarageID: garageID, Fields: valid()})
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		inactive := sampleUnit()
		inactive.IsActive = false
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(inactive, nil).Once()

		_, err = uc.CreatePart(t.Context(), usecase.CreatePartRequest{GarageID: garageID, Fields: valid()})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, catID).Return(&domain.Category{ID: catID, IsActive: false}, nil).Once()

		_, err = uc.CreatePart(t.Context(), usecase.CreatePartRequest{GarageID: garageID, Fields: valid()})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, catID).Return(nil, domain.ErrCategoryNotFound).Once()

		_, err = uc.CreatePart(t.Context(), usecase.CreatePartRequest{GarageID: garageID, Fields: valid()})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("duplicate SKU and storage failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, catID).Return(&domain.Category{ID: catID, IsActive: true}, nil).Once()
		d.Repo.EXPECT().CreatePart(mock.Anything, mock.Anything).Return(domain.ErrSKUTaken).Once()

		_, err := uc.CreatePart(t.Context(), usecase.CreatePartRequest{GarageID: garageID, Fields: valid()})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})
}

func TestUseCase_GetPart(t *testing.T) {
	t.Parallel()

	t.Run("attaches image URLs", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		p := samplePart()
		p.Images = []domain.Image{{FileID: fileID}}
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(p, nil).Once()
		d.Files.EXPECT().URLs(mock.Anything, garageID, []string{fileID}).Return(map[string]string{fileID: "https://cdn/a.png"}, nil).Once()

		got, err := uc.GetPart(t.Context(), garageID, partID)
		require.NoError(t, err)
		assert.Equal(t, "https://cdn/a.png", got.Images[0].URL)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(nil, domain.ErrPartNotFound).Once()

		_, err := uc.GetPart(t.Context(), garageID, partID)
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		p := samplePart()
		p.Images = []domain.Image{{FileID: fileID}}
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(p, nil).Once()
		d.Files.EXPECT().URLs(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.GetPart(t.Context(), garageID, partID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_UpdatePart(t *testing.T) {
	t.Parallel()

	t.Run("updates plain fields and clears the category", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		p := samplePart()
		p.CategoryID, p.CategoryName = new(catID), "Beverages"
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(p, nil).Once()
		d.Repo.EXPECT().UpdatePart(mock.Anything, mock.MatchedBy(func(p *domain.Part) bool {
			return p.Name == "Renamed" && p.CategoryID == nil && p.SalePrice == 20_000 && !p.IsActive && *p.UpdatedBy == actorID
		})).Return(nil).Once()
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()

		_, err := uc.UpdatePart(t.Context(), usecase.UpdatePartRequest{
			GarageID: garageID, PartID: partID, ActorID: actorID, Fields: usecase.PartFields{
				Name: new("Renamed"), CategoryID: new(""), SalePrice: new(int64(20_000)), IsActive: new(false),
			},
		})
		require.NoError(t, err)
	})

	t.Run("keeping the same category or base unit asks nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		p := samplePart()
		p.CategoryID = new(catID)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(p, nil).Twice()
		d.Repo.EXPECT().UpdatePart(mock.Anything, mock.Anything).Return(nil).Once()

		_, err := uc.UpdatePart(t.Context(), usecase.UpdatePartRequest{
			GarageID: garageID, PartID: partID, Fields: usecase.PartFields{CategoryID: new(catID), BaseUnitID: new(unitID)},
		})
		require.NoError(t, err)
	})

	t.Run("the base unit changes while nothing uses the part", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Twice()
		d.Usage.EXPECT().PartInUse(mock.Anything, garageID, partID).Return(false, nil).Once()
		d.Repo.EXPECT().PartUsedByJobs(mock.Anything, garageID, partID).Return(false, nil).Once()
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, caseID).Return(sampleCase(), nil).Once()
		d.Repo.EXPECT().UpdatePart(mock.Anything, mock.MatchedBy(func(p *domain.Part) bool {
			return p.BaseUnitID == caseID && p.BaseUnitAbbreviation == "ctn"
		})).Return(nil).Once()

		_, err := uc.UpdatePart(t.Context(), usecase.UpdatePartRequest{
			GarageID: garageID, PartID: partID, Fields: usecase.PartFields{BaseUnitID: new(caseID)},
		})
		require.NoError(t, err)
	})

	t.Run("the base unit is frozen by packaging or use", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		p := samplePart()
		p.PackagingUnits = []domain.PackagingUnit{{UnitID: caseID, Factor: "24"}}
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(p, nil).Once()

		_, err := uc.UpdatePart(t.Context(), usecase.UpdatePartRequest{
			GarageID: garageID, PartID: partID, Fields: usecase.PartFields{BaseUnitID: new(caseID)},
		})
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Usage.EXPECT().PartInUse(mock.Anything, garageID, partID).Return(true, nil).Once()

		_, err = uc.UpdatePart(t.Context(), usecase.UpdatePartRequest{
			GarageID: garageID, PartID: partID, Fields: usecase.PartFields{BaseUnitID: new(caseID)},
		})
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Usage.EXPECT().PartInUse(mock.Anything, garageID, partID).Return(false, errBoom).Once()

		_, err = uc.UpdatePart(t.Context(), usecase.UpdatePartRequest{
			GarageID: garageID, PartID: partID, Fields: usecase.PartFields{BaseUnitID: new(caseID)},
		})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(nil, domain.ErrPartNotFound).Once()

		_, err := uc.UpdatePart(t.Context(), usecase.UpdatePartRequest{GarageID: garageID, PartID: partID})
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()

		_, err = uc.UpdatePart(t.Context(), usecase.UpdatePartRequest{GarageID: garageID, PartID: partID, Fields: usecase.PartFields{SKU: new("")}})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Repo.EXPECT().UpdatePart(mock.Anything, mock.Anything).Return(domain.ErrSKUTaken).Once()

		_, err = uc.UpdatePart(t.Context(), usecase.UpdatePartRequest{GarageID: garageID, PartID: partID, Fields: usecase.PartFields{SKU: new("DUP")}})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})
}

func TestUseCase_ListParts(t *testing.T) {
	t.Parallel()

	t.Run("a category filter includes its descendants", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(tree(), nil).Once()
		d.Repo.EXPECT().ListParts(mock.Anything, garageID, usecase.PartFilter{
			Query: "oil", CategoryIDs: []string{"oil", "synth", "leaf"}, Brand: "Shell", IncludeInactive: true, Limit: 20,
		}).Return([]*domain.Part{samplePart()}, 1, nil).Once()

		page, err := uc.ListParts(t.Context(), usecase.ListPartsRequest{
			GarageID: garageID, Query: " oil ", CategoryID: "oil", Brand: " Shell ", IncludeInactive: true,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, page.Total)
	})

	t.Run("without a category filter the tree is not read", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListParts(mock.Anything, garageID, usecase.PartFilter{Limit: 5, Offset: 10}).Return(nil, 0, nil).Once()

		_, err := uc.ListParts(t.Context(), usecase.ListPartsRequest{GarageID: garageID, PageRequest: usecase.PageRequest{Page: 3, PageSize: 5}})
		require.NoError(t, err)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(nil, errBoom).Once()

		_, err := uc.ListParts(t.Context(), usecase.ListPartsRequest{GarageID: garageID, CategoryID: "oil"})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().ListParts(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err = uc.ListParts(t.Context(), usecase.ListPartsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		p := samplePart()
		p.Images = []domain.Image{{FileID: fileID}}
		d.Repo.EXPECT().ListParts(mock.Anything, garageID, mock.Anything).Return([]*domain.Part{p}, 1, nil).Once()
		d.Files.EXPECT().URLs(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.ListParts(t.Context(), usecase.ListPartsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_PartsByIDs(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)

	got, err := uc.PartsByIDs(t.Context(), garageID, nil)
	require.NoError(t, err)
	assert.Empty(t, got)

	d.Repo.EXPECT().FindPartsByIDs(mock.Anything, garageID, []string{partID}).Return([]*domain.Part{samplePart()}, nil).Once()

	got, err = uc.PartsByIDs(t.Context(), garageID, []string{partID})
	require.NoError(t, err)
	assert.Len(t, got, 1)

	d.Repo.EXPECT().FindPartsByIDs(mock.Anything, garageID, []string{partID}).Return(nil, errBoom).Once()

	_, err = uc.PartsByIDs(t.Context(), garageID, []string{partID})
	assertAppError(t, err, apperror.CodeInternal)

	p := samplePart()
	p.Images = []domain.Image{{FileID: fileID}}
	d.Repo.EXPECT().FindPartsByIDs(mock.Anything, garageID, []string{partID}).Return([]*domain.Part{p}, nil).Once()
	d.Files.EXPECT().URLs(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

	_, err = uc.PartsByIDs(t.Context(), garageID, []string{partID})
	assertAppError(t, err, apperror.CodeInternal)
}

func TestUseCase_DeletePart(t *testing.T) {
	t.Parallel()

	req := usecase.DeleteRequest{GarageID: garageID, ID: partID, ActorID: actorID}

	t.Run("an unused part is deleted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Usage.EXPECT().PartInUse(mock.Anything, garageID, partID).Return(false, nil).Once()
		d.Repo.EXPECT().PartUsedByJobs(mock.Anything, garageID, partID).Return(false, nil).Once()
		d.Repo.EXPECT().SoftDeletePart(mock.Anything, garageID, partID, actorID).Return(nil).Once()

		outcome, err := uc.DeletePart(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deleted, outcome)
	})

	t.Run("a used part is deactivated", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Usage.EXPECT().PartInUse(mock.Anything, garageID, partID).Return(true, nil).Once()
		d.Repo.EXPECT().SetPartActive(mock.Anything, garageID, partID, false, actorID).Return(nil).Once()

		outcome, err := uc.DeletePart(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(nil, domain.ErrPartNotFound).Once()

		_, err := uc.DeletePart(t.Context(), req)
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Usage.EXPECT().PartInUse(mock.Anything, garageID, partID).Return(false, errBoom).Once()

		_, err = uc.DeletePart(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_ActivatePart(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().SetPartActive(mock.Anything, garageID, partID, true, actorID).Return(nil).Once()
	d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()

	_, err := uc.ActivatePart(t.Context(), garageID, partID, actorID)
	require.NoError(t, err)

	d.Repo.EXPECT().SetPartActive(mock.Anything, garageID, partID, true, actorID).Return(domain.ErrPartNotFound).Once()

	_, err = uc.ActivatePart(t.Context(), garageID, partID, actorID)
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestUseCase_SetPartUnits(t *testing.T) {
	t.Parallel()

	t.Run("replaces the packaging table", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Twice()
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, caseID).Return(sampleCase(), nil).Once()
		d.Repo.EXPECT().ReplacePackaging(mock.Anything, garageID, partID, actorID, []domain.PackagingUnit{
			{UnitID: caseID, Factor: "24", SalePrice: new(int64(340_000))},
		}).Return(nil).Once()

		_, err := uc.SetPartUnits(t.Context(), garageID, partID, actorID, []domain.PackagingUnit{
			{UnitID: caseID, Factor: "24.000", SalePrice: new(int64(340_000))},
		})
		require.NoError(t, err)
	})

	t.Run("an empty table clears it", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Twice()
		d.Repo.EXPECT().ReplacePackaging(mock.Anything, garageID, partID, actorID, []domain.PackagingUnit{}).Return(nil).Once()

		_, err := uc.SetPartUnits(t.Context(), garageID, partID, actorID, nil)
		require.NoError(t, err)
	})

	t.Run("rejections", func(t *testing.T) {
		t.Parallel()

		inactive := sampleCase()
		inactive.IsActive = false

		for name, tc := range map[string]struct {
			units []domain.PackagingUnit
			setup func(d *deps)
			code  apperror.Code
		}{
			"the base unit itself": {[]domain.PackagingUnit{{UnitID: unitID, Factor: "1"}}, nil, apperror.CodeInvalidArgument},
			"a unit twice": {
				[]domain.PackagingUnit{{UnitID: caseID, Factor: "2"}, {UnitID: caseID, Factor: "3"}},
				func(d *deps) {
					d.Repo.EXPECT().FindUnit(mock.Anything, garageID, caseID).Return(sampleCase(), nil).Maybe()
				},
				apperror.CodeInvalidArgument,
			},
			"a zero factor":    {[]domain.PackagingUnit{{UnitID: caseID, Factor: "0"}}, nil, apperror.CodeInvalidArgument},
			"a negative price": {[]domain.PackagingUnit{{UnitID: caseID, Factor: "2", SalePrice: new(int64(-1))}}, nil, apperror.CodeInvalidArgument},
			"an unknown unit": {
				[]domain.PackagingUnit{{UnitID: caseID, Factor: "2"}},
				func(d *deps) {
					d.Repo.EXPECT().FindUnit(mock.Anything, garageID, caseID).Return(nil, domain.ErrUnitNotFound).Once()
				},
				apperror.CodeNotFound,
			},
			"an inactive unit": {
				[]domain.PackagingUnit{{UnitID: caseID, Factor: "2"}},
				func(d *deps) { d.Repo.EXPECT().FindUnit(mock.Anything, garageID, caseID).Return(inactive, nil).Once() },
				apperror.CodeInvalidArgument,
			},
		} {
			uc, d := newUseCase(t)
			d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()

			if tc.setup != nil {
				tc.setup(d)
			}

			_, err := uc.SetPartUnits(t.Context(), garageID, partID, actorID, tc.units)
			assertAppError(t, err, tc.code)

			_ = name
		}
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(nil, domain.ErrPartNotFound).Once()

		_, err := uc.SetPartUnits(t.Context(), garageID, partID, actorID, nil)
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Repo.EXPECT().ReplacePackaging(mock.Anything, garageID, partID, actorID, mock.Anything).Return(errBoom).Once()

		_, err = uc.SetPartUnits(t.Context(), garageID, partID, actorID, nil)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_AddPartImage(t *testing.T) {
	t.Parallel()

	t.Run("attaches an uploaded image", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Twice()
		d.Repo.EXPECT().ListImages(mock.Anything, garageID, partID).Return([]usecase.ImageRef{{FileID: "a"}, {FileID: "b"}}, nil).Once()
		d.Files.EXPECT().PartImage(mock.Anything, garageID, partID, fileID).Return(nil).Once()
		d.Repo.EXPECT().FileAttached(mock.Anything, garageID, fileID).Return(false, nil).Once()
		d.Repo.EXPECT().AddImage(mock.Anything, garageID, partID, fileID, actorID).Return(nil).Once()

		_, err := uc.AddPartImage(t.Context(), garageID, partID, fileID, actorID)
		require.NoError(t, err)
	})

	t.Run("rejections", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(nil, domain.ErrPartNotFound).Once()

		_, err := uc.AddPartImage(t.Context(), garageID, partID, fileID, actorID)
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Repo.EXPECT().ListImages(mock.Anything, garageID, partID).Return(nil, errBoom).Once()

		_, err = uc.AddPartImage(t.Context(), garageID, partID, fileID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Repo.EXPECT().ListImages(mock.Anything, garageID, partID).Return([]usecase.ImageRef{{}, {}, {}}, nil).Once()

		_, err = uc.AddPartImage(t.Context(), garageID, partID, fileID, actorID)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Repo.EXPECT().ListImages(mock.Anything, garageID, partID).Return(nil, nil).Once()
		d.Files.EXPECT().PartImage(mock.Anything, garageID, partID, fileID).Return(domain.ErrNotPartImage).Once()

		_, err = uc.AddPartImage(t.Context(), garageID, partID, fileID, actorID)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Repo.EXPECT().ListImages(mock.Anything, garageID, partID).Return(nil, nil).Once()
		d.Files.EXPECT().PartImage(mock.Anything, garageID, partID, fileID).Return(nil).Once()
		d.Repo.EXPECT().FileAttached(mock.Anything, garageID, fileID).Return(true, nil).Once()

		_, err = uc.AddPartImage(t.Context(), garageID, partID, fileID, actorID)
		assertAppError(t, err, apperror.CodeAlreadyExists)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Repo.EXPECT().ListImages(mock.Anything, garageID, partID).Return(nil, nil).Once()
		d.Files.EXPECT().PartImage(mock.Anything, garageID, partID, fileID).Return(nil).Once()
		d.Repo.EXPECT().FileAttached(mock.Anything, garageID, fileID).Return(false, errBoom).Once()

		_, err = uc.AddPartImage(t.Context(), garageID, partID, fileID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
		d.Repo.EXPECT().ListImages(mock.Anything, garageID, partID).Return(nil, nil).Once()
		d.Files.EXPECT().PartImage(mock.Anything, garageID, partID, fileID).Return(nil).Once()
		d.Repo.EXPECT().FileAttached(mock.Anything, garageID, fileID).Return(false, nil).Once()
		d.Repo.EXPECT().AddImage(mock.Anything, garageID, partID, fileID, actorID).Return(errBoom).Once()

		_, err = uc.AddPartImage(t.Context(), garageID, partID, fileID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUseCase_RemovePartImage(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().RemoveImage(mock.Anything, garageID, partID, fileID).Return(nil).Once()
	d.Files.EXPECT().Release(mock.Anything, garageID, fileID, actorID).Return(nil).Once()
	d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()

	_, err := uc.RemovePartImage(t.Context(), garageID, partID, fileID, actorID)
	require.NoError(t, err)

	d.Repo.EXPECT().RemoveImage(mock.Anything, garageID, partID, fileID).Return(domain.ErrImageNotFound).Once()

	_, err = uc.RemovePartImage(t.Context(), garageID, partID, fileID, actorID)
	assertAppError(t, err, apperror.CodeNotFound)

	d.Repo.EXPECT().RemoveImage(mock.Anything, garageID, partID, fileID).Return(nil).Once()
	d.Files.EXPECT().Release(mock.Anything, garageID, fileID, actorID).Return(errBoom).Once()

	_, err = uc.RemovePartImage(t.Context(), garageID, partID, fileID, actorID)
	assertAppError(t, err, apperror.CodeInternal)
}
