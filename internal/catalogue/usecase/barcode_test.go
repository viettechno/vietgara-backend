package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// cartonPart is a part with the carton packaging unit and a barcode for it.
func cartonPart() *domain.Part {
	p := samplePart()
	p.PackagingUnits = []domain.PackagingUnit{{UnitID: caseID, Factor: "24"}}

	return p
}

func TestSetPartBarcodes(t *testing.T) {
	t.Parallel()

	carton := caseID
	unknown := catID

	tests := map[string]struct {
		codes  []domain.PartBarcode
		code   apperror.Code
		reason string
	}{
		"base and carton codes": {codes: []domain.PartBarcode{{Barcode: " 8930001 "}, {Barcode: "8930024", UnitID: &carton}}},
		"no codes clears them":  {codes: nil},
		"empty code":            {codes: []domain.PartBarcode{{Barcode: " "}}, code: apperror.CodeInvalidArgument, reason: "BARCODE_INVALID"},
		"bad characters":        {codes: []domain.PartBarcode{{Barcode: "ab_cd"}}, code: apperror.CodeInvalidArgument, reason: "BARCODE_INVALID"},
		"same code twice, any case": {
			codes:  []domain.PartBarcode{{Barcode: "AbC"}, {Barcode: "aBc"}},
			code:   apperror.CodeAlreadyExists,
			reason: "BARCODE_ALREADY_USED",
		},
		"unit is not a packaging unit": {
			codes:  []domain.PartBarcode{{Barcode: "1", UnitID: &unknown}},
			code:   apperror.CodeInvalidArgument,
			reason: "BARCODE_INVALID",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			uc, d := newUseCase(t)
			d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(cartonPart(), nil)

			if tt.code == "" {
				d.Repo.EXPECT().ReplaceBarcodes(mock.Anything, garageID, partID, actorID, mock.Anything).Return(nil)
			}

			p, err := uc.SetPartBarcodes(t.Context(), garageID, partID, actorID, tt.codes)

			if tt.code != "" {
				assertAppError(t, err, tt.code)
				appErr, _ := apperror.As(err)
				assert.Equal(t, tt.reason, appErr.Reason)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, partID, p.ID)
		})
	}
}

func TestSetPartBarcodesTrimsCodes(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(cartonPart(), nil)
	d.Repo.EXPECT().ReplaceBarcodes(mock.Anything, garageID, partID, actorID, []domain.PartBarcode{{Barcode: "8930001"}}).Return(nil)

	_, err := uc.SetPartBarcodes(t.Context(), garageID, partID, actorID, []domain.PartBarcode{{Barcode: "  8930001  "}})
	require.NoError(t, err)
}

func TestSetPartBarcodesLimits(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(cartonPart(), nil)

	tooMany := make([]domain.PartBarcode, 0, domain.MaxPartBarcodes+1)
	for i := range domain.MaxPartBarcodes + 1 {
		tooMany = append(tooMany, domain.PartBarcode{Barcode: string(rune('a'+i%26)) + string(rune('a'+i/26))})
	}

	_, err := uc.SetPartBarcodes(t.Context(), garageID, partID, actorID, tooMany)
	assertAppError(t, err, apperror.CodeInvalidArgument)
}

func TestSetPartBarcodesFailures(t *testing.T) {
	t.Parallel()

	t.Run("part not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(nil, domain.ErrPartNotFound)

		_, err := uc.SetPartBarcodes(t.Context(), garageID, partID, actorID, nil)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("code used by another part", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(cartonPart(), nil)
		d.Repo.EXPECT().ReplaceBarcodes(mock.Anything, garageID, partID, actorID, mock.Anything).Return(domain.ErrBarcodeAlreadyUsed)

		_, err := uc.SetPartBarcodes(t.Context(), garageID, partID, actorID, []domain.PartBarcode{{Barcode: "1"}})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})

	t.Run("repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(cartonPart(), nil)
		d.Repo.EXPECT().ReplaceBarcodes(mock.Anything, garageID, partID, actorID, mock.Anything).Return(errBoom)

		_, err := uc.SetPartBarcodes(t.Context(), garageID, partID, actorID, []domain.PartBarcode{{Barcode: "1"}})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestSetPartUnitsKeepsPackagingUnitsWithBarcodes(t *testing.T) {
	t.Parallel()

	carton := caseID
	withCode := cartonPart()
	withCode.Barcodes = []domain.PartBarcode{{Barcode: "1"}, {Barcode: "2", UnitID: &carton}}

	t.Run("removing the carton is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(withCode, nil)

		_, err := uc.SetPartUnits(t.Context(), garageID, partID, actorID, nil)
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		appErr, _ := apperror.As(err)
		assert.Equal(t, "PACKAGING_UNIT_HAS_BARCODE", appErr.Reason)
	})

	t.Run("keeping the carton is fine", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(withCode, nil)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, caseID).Return(sampleCase(), nil)
		d.Repo.EXPECT().ReplacePackaging(mock.Anything, garageID, partID, actorID, mock.Anything).Return(nil)

		_, err := uc.SetPartUnits(t.Context(), garageID, partID, actorID, []domain.PackagingUnit{{UnitID: caseID, Factor: "12"}})
		require.NoError(t, err)
	})
}

func TestLookupPartByBarcode(t *testing.T) {
	t.Parallel()

	carton := caseID

	t.Run("found, with the unit of the code", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindBarcode(mock.Anything, garageID, "8930024").
			Return(cartonPart(), &domain.PartBarcode{Barcode: "8930024", UnitID: &carton}, nil)

		p, code, err := uc.LookupPartByBarcode(t.Context(), garageID, " 8930024 ")
		require.NoError(t, err)
		assert.Equal(t, partID, p.ID)
		assert.Equal(t, caseID, *code.UnitID)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindBarcode(mock.Anything, garageID, "0").Return(nil, nil, domain.ErrBarcodeNotFound)

		_, _, err := uc.LookupPartByBarcode(t.Context(), garageID, "0")
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("a blank or oversized code never reaches the repository", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		for _, bad := range []string{"", "   ", strings.Repeat("9", domain.MaxBarcodeLen+1)} {
			_, _, err := uc.LookupPartByBarcode(t.Context(), garageID, bad)
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}
	})

	t.Run("a scan with characters the garage cannot store is simply not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindBarcode(mock.Anything, garageID, "a_b:ü").Return(nil, nil, domain.ErrBarcodeNotFound)

		_, _, err := uc.LookupPartByBarcode(t.Context(), garageID, "a_b:ü")
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("images failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		withImage := samplePart()
		withImage.Images = []domain.Image{{FileID: fileID}}

		d.Repo.EXPECT().FindBarcode(mock.Anything, garageID, "1").Return(withImage, &domain.PartBarcode{Barcode: "1"}, nil)
		d.Files.EXPECT().URLs(mock.Anything, garageID, []string{fileID}).Return(nil, errBoom)

		_, _, err := uc.LookupPartByBarcode(t.Context(), garageID, "1")
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestPartsBySKUsAndBarcodes(t *testing.T) {
	t.Parallel()

	t.Run("no SKUs, no query", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		got, err := uc.PartsBySKUs(t.Context(), garageID, nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("SKUs", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPartsBySKUs(mock.Anything, garageID, []string{"beer-330"}).Return([]*domain.Part{samplePart()}, nil)

		got, err := uc.PartsBySKUs(t.Context(), garageID, []string{"beer-330"})
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("SKU failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPartsBySKUs(mock.Anything, garageID, mock.Anything).Return(nil, errBoom)

		_, err := uc.PartsBySKUs(t.Context(), garageID, []string{"x"})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("codes of one part load it once", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().BarcodesByCode(mock.Anything, garageID, []string{"1", "2"}).
			Return(map[string]string{"1": partID, "2": partID}, nil)
		d.Repo.EXPECT().FindPartsByIDs(mock.Anything, garageID, []string{partID}).Return([]*domain.Part{samplePart()}, nil)

		got, err := uc.PartsByBarcodes(t.Context(), garageID, []string{"1", "2"})
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("no code matches", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().BarcodesByCode(mock.Anything, garageID, mock.Anything).Return(map[string]string{}, nil)

		got, err := uc.PartsByBarcodes(t.Context(), garageID, []string{"1"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("barcode lookup failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().BarcodesByCode(mock.Anything, garageID, []string{"1"}).Return(nil, errBoom).Once()
		d.Repo.EXPECT().BarcodesByCode(mock.Anything, garageID, []string{"2"}).Return(map[string]string{"2": partID}, nil).Once()
		d.Repo.EXPECT().FindPartsByIDs(mock.Anything, garageID, []string{partID}).Return(nil, errBoom).Once()

		_, err := uc.PartsByBarcodes(t.Context(), garageID, []string{"1"})
		assertAppError(t, err, apperror.CodeInternal)

		_, err = uc.PartsByBarcodes(t.Context(), garageID, []string{"2"})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("existing barcodes", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().BarcodesByCode(mock.Anything, garageID, []string{"1"}).Return(map[string]string{"1": partID}, nil).Once()
		d.Repo.EXPECT().BarcodesByCode(mock.Anything, garageID, []string{"2"}).Return(nil, errBoom).Once()

		got, err := uc.BarcodesByCode(t.Context(), garageID, []string{"1"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"1": partID}, got)

		_, err = uc.BarcodesByCode(t.Context(), garageID, []string{"2"})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

//nolint:funlen // one table of bulk-create cases
func TestCreateParts(t *testing.T) {
	t.Parallel()

	carton := caseID
	two := domain.PartBarcode{Barcode: "8930024", UnitID: &carton}

	valid := func() []usecase.NewPart {
		return []usecase.NewPart{
			{
				SKU: " BEER-1 ", Name: "Beer", Brand: "Tiger", CategoryID: catID, BaseUnitID: unitID,
				Packaging: []domain.PackagingUnit{{UnitID: caseID, Factor: "24"}},
				Barcodes:  []domain.PartBarcode{{Barcode: " 8930001 "}, two},
			},
			{SKU: "BEER-2", Name: "Beer 2", CategoryID: catID, BaseUnitID: unitID, Barcodes: []domain.PartBarcode{{Barcode: "8930002"}}},
		}
	}

	t.Run("creates parts, packaging and barcodes in one call, loading units and categories once", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, caseID).Return(sampleCase(), nil).Once()
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, catID).Return(&domain.Category{ID: catID, IsActive: true}, nil).Once()

		var (
			gotParts    []*domain.Part
			gotPackages []usecase.PackagingRow
			gotCodes    []usecase.BarcodeRow
		)

		d.Repo.EXPECT().CreateParts(mock.Anything, garageID, actorID, mock.Anything, mock.Anything, mock.Anything).
			Run(func(_ context.Context, _, _ string, parts []*domain.Part, packaging []usecase.PackagingRow, barcodes []usecase.BarcodeRow) {
				gotParts, gotPackages, gotCodes = parts, packaging, barcodes
			}).Return(nil).Once()

		ids, err := uc.CreateParts(t.Context(), garageID, actorID, valid())
		require.NoError(t, err)

		require.Len(t, ids, 2)
		require.Len(t, gotParts, 2)
		assert.Equal(t, ids[0], gotParts[0].ID)
		assert.Equal(t, "BEER-1", gotParts[0].SKU, "trimmed")
		assert.True(t, gotParts[0].IsActive)
		assert.Equal(t, catID, *gotParts[1].CategoryID)
		require.Len(t, gotPackages, 1)
		assert.Equal(t, ids[0], gotPackages[0].PartID)
		assert.Equal(t, "24", gotPackages[0].Unit.Factor)
		require.Len(t, gotCodes, 3)
		assert.Equal(t, "8930001", gotCodes[0].Barcode.Barcode, "trimmed")
		assert.Equal(t, ids[1], gotCodes[2].PartID)
	})

	t.Run("applies the rules of the single-part calls", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			mutate func(p []usecase.NewPart)
			code   apperror.Code
		}{
			"no SKU":                   {func(p []usecase.NewPart) { p[0].SKU = "" }, apperror.CodeInvalidArgument},
			"no name":                  {func(p []usecase.NewPart) { p[0].Name = "" }, apperror.CodeInvalidArgument},
			"no base unit":             {func(p []usecase.NewPart) { p[0].BaseUnitID = "" }, apperror.CodeInvalidArgument},
			"base unit also packaging": {func(p []usecase.NewPart) { p[0].Packaging[0].UnitID = unitID }, apperror.CodeInvalidArgument},
			"bad factor":               {func(p []usecase.NewPart) { p[0].Packaging[0].Factor = "0" }, apperror.CodeInvalidArgument},
			"bad barcode":              {func(p []usecase.NewPart) { p[0].Barcodes[0].Barcode = "a_b" }, apperror.CodeInvalidArgument},
			"unit without packaging":   {func(p []usecase.NewPart) { p[1].Barcodes[0].UnitID = &carton }, apperror.CodeInvalidArgument},
			"code twice in one part":   {func(p []usecase.NewPart) { p[0].Barcodes[0].Barcode = "8930024" }, apperror.CodeAlreadyExists},
			"code twice in the batch":  {func(p []usecase.NewPart) { p[1].Barcodes[0].Barcode = "8930001" }, apperror.CodeAlreadyExists},
			"too long a SKU":           {func(p []usecase.NewPart) { p[0].SKU = strings.Repeat("x", 60) }, apperror.CodeInvalidArgument},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				uc, d := newUseCase(t)
				d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Maybe()
				d.Repo.EXPECT().FindUnit(mock.Anything, garageID, caseID).Return(sampleCase(), nil).Maybe()
				d.Repo.EXPECT().FindCategory(mock.Anything, garageID, catID).Return(&domain.Category{ID: catID, IsActive: true}, nil).Maybe()

				parts := valid()
				tt.mutate(parts)

				_, err := uc.CreateParts(t.Context(), garageID, actorID, parts)
				assertAppError(t, err, tt.code)
			})
		}
	})

	t.Run("refuses units and categories that cannot be used", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		inactive := sampleUnit()
		inactive.IsActive = false
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(inactive, nil).Once()

		_, err := uc.CreateParts(t.Context(), garageID, actorID, valid())
		assertAppError(t, err, apperror.CodeInvalidArgument)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(nil, domain.ErrUnitNotFound).Once()

		_, err = uc.CreateParts(t.Context(), garageID, actorID, valid())
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, catID).Return(&domain.Category{ID: catID}, nil).Once()

		_, err = uc.CreateParts(t.Context(), garageID, actorID, valid())
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("a SKU or code taken meanwhile is a conflict", func(t *testing.T) {
		t.Parallel()

		for _, taken := range []error{domain.ErrSKUTaken, domain.ErrBarcodeAlreadyUsed} {
			uc, d := newUseCase(t)
			d.Repo.EXPECT().FindUnit(mock.Anything, garageID, mock.Anything).Return(sampleUnit(), nil).Maybe()
			d.Repo.EXPECT().FindCategory(mock.Anything, garageID, catID).Return(&domain.Category{ID: catID, IsActive: true}, nil).Maybe()
			d.Repo.EXPECT().CreateParts(mock.Anything, garageID, actorID, mock.Anything, mock.Anything, mock.Anything).Return(taken).Once()

			_, err := uc.CreateParts(t.Context(), garageID, actorID, valid())
			assertAppError(t, err, apperror.CodeAlreadyExists)
		}
	})
}
