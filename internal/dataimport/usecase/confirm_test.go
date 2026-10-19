package usecase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/template"
	"github.com/viettechno/vietgara-backend/internal/dataimport/usecase"
)

func TestDownloadTemplate(t *testing.T) {
	uc, _ := newDeps(t)

	data, name, err := uc.DownloadTemplate(domain.TypeParts)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.Equal(t, "vietgara-parts-template.xlsx", name)

	_, _, err = uc.DownloadTemplate(domain.Type("nope"))
	require.Error(t, err)
}

func TestConfirmPermissionRequired(t *testing.T) {
	uc, d := newDeps(t)
	d.reader.sheet = sheet(domain.TypeParts, partRow())

	_, err := uc.Confirm(t.Context(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.TypeParts, Content: []byte("xlsx"),
		Permissions: usecase.Permissions{},
	})

	require.Equal(t, domain.CodePermissionRequired, reasonOf(t, err))
}

func TestConfirmAlreadyImported(t *testing.T) {
	uc, d := newDeps(t)
	d.reader.sheet = sheet(domain.TypeParts, partRow())
	d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&usecase.Run{ID: "run-1"}, nil)

	_, err := uc.Confirm(t.Context(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.TypeParts, FileName: "f.xlsx", Content: []byte("xlsx"),
		Permissions: allPermissions,
	})

	require.Equal(t, "IMPORT_FILE_ALREADY_IMPORTED", reasonOf(t, err))
}

func TestConfirmInvalidFile(t *testing.T) {
	uc, d := newDeps(t)
	d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)

	row := partRow()
	row[template.ColName] = ""
	d.reader.sheet = sheet(domain.TypeParts, row)
	catalogueState{}.expect(d)
	expectSuppliers(d, nil)

	_, err := uc.Confirm(t.Context(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.TypeParts, FileName: "f.xlsx", Content: []byte("xlsx"),
		Permissions: allPermissions,
	})

	require.Equal(t, "IMPORT_FILE_INVALID", reasonOf(t, err))
}

func TestConfirmPartsApplies(t *testing.T) {
	uc, d := newDeps(t)

	row := partRow()
	row[template.ColBarcodes] = "BRK-1"
	row[template.ColPackagingUnits] = "Case=24"

	d.reader.sheet = sheet(domain.TypeParts, row)
	catalogueState{
		units: []usecase.UnitInfo{{ID: unitID, Name: "Piece", Abbreviation: "pc", IsActive: true}},
	}.expect(d)

	d.catalogue.EXPECT().CreateUnit(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(usecase.UnitInfo{ID: catID, Name: "Case", Abbreviation: "Case", IsActive: true}, nil)
	d.catalogue.EXPECT().CreateParts(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return([]string{partID}, nil)
	d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)

	var created *usecase.Run

	d.runs.EXPECT().Create(mock.Anything, mock.MatchedBy(func(r *usecase.Run) bool {
		created = r
		return true
	})).Return(nil)
	d.audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil)

	result, err := uc.Confirm(t.Context(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.TypeParts, FileName: "parts.xlsx",
		Content: []byte("xlsx"), Permissions: allPermissions,
	})
	require.NoError(t, err)

	require.Equal(t, 1, result.RowsImported)
	require.Equal(t, domain.TypeParts, result.Type)
	require.NotNil(t, created)
	require.Equal(t, "parts.xlsx", created.FileName)
	require.Equal(t, 1, created.RowCount)
	require.NotNil(t, created.CreatedBy)
}

func TestConfirmGoodsReceiptsApplies(t *testing.T) {
	uc, d := newDeps(t)

	row := map[string]string{
		template.ColSKU: "BRK-001", template.ColQuantity: "5", template.ColSupplier: "ACME",
		template.ColReceiptDate: "2026-02-03", template.ColUnitCost: "2000",
	}
	d.reader.sheet = sheet(domain.TypeGoodsReceipts, row)
	activePart().expect(d)
	expectSuppliers(d, map[string]string{"ACME": supplier})

	d.inventory.EXPECT().CreateGoodsReceipt(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, false).
		Return(usecase.Document{ID: "doc-1", Number: "GR-0001"}, nil)
	d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	d.runs.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)
	d.audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil)

	result, err := uc.Confirm(t.Context(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.TypeGoodsReceipts, FileName: "gr.xlsx",
		Content: []byte("xlsx"), Permissions: allPermissions,
	})
	require.NoError(t, err)

	require.Equal(t, "doc-1", result.DocumentID)
	require.Equal(t, "GR-0001", result.DocumentNumber)
	require.Equal(t, 1, result.RowsImported)
}

func TestConfirmStockCountsApplies(t *testing.T) {
	uc, d := newDeps(t)

	row := map[string]string{template.ColName: "Cycle", template.ColSKU: "BRK-001", template.ColCountedQuantity: "4"}
	d.reader.sheet = sheet(domain.TypeStockCounts, row)
	activePart().expect(d)

	d.inventory.EXPECT().CreateStockCount(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(usecase.Document{ID: "doc-2", Number: "SC-0001"}, nil)
	d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	d.runs.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)
	d.audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil)

	result, err := uc.Confirm(t.Context(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.TypeStockCounts, FileName: "sc.xlsx",
		Content: []byte("xlsx"), Permissions: allPermissions,
	})
	require.NoError(t, err)

	require.Equal(t, "doc-2", result.DocumentID)
	require.Equal(t, 1, result.RowsImported)
}

func TestConfirmPartsBindsPackagingBarcodeToItsUnit(t *testing.T) {
	uc, d := newDeps(t)

	row := partRow()
	row[template.ColBarcodes] = "111; 222@case"
	row[template.ColPackagingUnits] = "Case=24"

	d.reader.sheet = sheet(domain.TypeParts, row)
	catalogueState{units: []usecase.UnitInfo{{ID: unitID, Name: "Piece", Abbreviation: "pc", IsActive: true}}}.expect(d)

	d.catalogue.EXPECT().CreateUnit(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(usecase.UnitInfo{ID: catID, Name: "Case", Abbreviation: "Case", IsActive: true}, nil)

	var created []usecase.NewPart

	d.catalogue.EXPECT().CreateParts(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ context.Context, _, _ string, in []usecase.NewPart) { created = in }).Return([]string{partID}, nil)
	d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	d.runs.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)
	d.audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil)

	_, err := uc.Confirm(t.Context(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.TypeParts, FileName: "parts.xlsx",
		Content: []byte("xlsx"), Permissions: allPermissions,
	})
	require.NoError(t, err)

	require.Len(t, created, 1)
	require.Equal(t, unitID, created[0].BaseUnitID)
	require.Equal(t, []usecase.PackagingUnit{{UnitID: catID, Factor: "24"}}, created[0].Packaging)

	codes := created[0].Barcodes
	require.Len(t, codes, 2)
	require.Equal(t, "111", codes[0].Barcode)
	require.Nil(t, codes[0].UnitID, "a plain code stands for the base unit")
	require.Equal(t, "222", codes[1].Barcode)
	require.NotNil(t, codes[1].UnitID)
	require.Equal(t, catID, *codes[1].UnitID)
}

func TestConfirmReceiptUsesTheFirstHeaderValues(t *testing.T) {
	uc, d := newDeps(t)

	line := func(date, note string) map[string]string {
		return map[string]string{
			template.ColSKU: "BRK-001", template.ColQuantity: "1", template.ColSupplier: "ACME",
			template.ColReceiptDate: date, template.ColDeliveryNote: note,
		}
	}

	d.reader.sheet = sheet(domain.TypeGoodsReceipts, line("", "DN-7"), line("2026-02-03", ""))
	activePart().expect(d)
	expectSuppliers(d, map[string]string{"ACME": supplier})

	var in usecase.ReceiptInput

	d.inventory.EXPECT().CreateGoodsReceipt(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, false).
		Run(func(_ context.Context, _, _ string, _ bool, got usecase.ReceiptInput, _ bool) { in = got }).
		Return(usecase.Document{ID: "doc-1", Number: "GR-0001"}, nil)
	d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	d.runs.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)
	d.audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil)

	_, err := uc.Confirm(t.Context(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.TypeGoodsReceipts, FileName: "gr.xlsx",
		Content: []byte("xlsx"), Permissions: allPermissions,
	})
	require.NoError(t, err)

	require.Equal(t, "2026-02-03", in.ReceiptDate)
	require.Equal(t, "DN-7", in.DeliveryNote)
	require.Len(t, in.Lines, 2)
}

// confirmable is a goods receipt file that passes every check, so a failure
// of what comes after the checks can be provoked.
func confirmable(t *testing.T) (*usecase.UseCase, *deps, usecase.Request) {
	t.Helper()

	uc, d := newDeps(t)

	d.reader.sheet = sheet(domain.TypeStockCounts, map[string]string{template.ColSKU: "BRK-001", template.ColCountedQuantity: "4"})
	activePart().expect(d)
	d.inventory.EXPECT().CreateStockCount(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(usecase.Document{ID: "doc-2", Number: "SC-0001"}, nil)

	return uc, d, usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.TypeStockCounts, FileName: "sc.xlsx",
		Content: []byte("xlsx"), Permissions: allPermissions,
	}
}

func TestConfirmFailuresAfterTheChecks(t *testing.T) {
	t.Run("the history cannot be read", func(t *testing.T) {
		uc, d := newDeps(t)
		d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, errBoom)

		_, err := uc.Confirm(t.Context(), usecase.Request{
			GarageID: garageID, Type: domain.TypeParts, Content: []byte("xlsx"), Permissions: allPermissions,
		})
		require.Error(t, err)
	})

	t.Run("the history cannot be written", func(t *testing.T) {
		uc, d, req := confirmable(t)
		d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
		d.runs.EXPECT().Create(mock.Anything, mock.Anything).Return(errBoom)

		_, err := uc.Confirm(t.Context(), req)
		require.Error(t, err)
	})

	t.Run("a concurrent confirmation of the same file", func(t *testing.T) {
		uc, d, req := confirmable(t)
		d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
		d.runs.EXPECT().Create(mock.Anything, mock.Anything).Return(domain.ErrFileAlreadyImported)

		_, err := uc.Confirm(t.Context(), req)
		require.Equal(t, "IMPORT_FILE_ALREADY_IMPORTED", reasonOf(t, err))
	})

	t.Run("the audit entry cannot be written", func(t *testing.T) {
		uc, d, req := confirmable(t)
		d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
		d.runs.EXPECT().Create(mock.Anything, mock.Anything).Return(nil)
		d.audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom)

		_, err := uc.Confirm(t.Context(), req)
		require.Error(t, err, "an unaudited import is not imported")
	})

	t.Run("the document cannot be created", func(t *testing.T) {
		uc, d := newDeps(t)
		d.reader.sheet = sheet(domain.TypeStockCounts, map[string]string{template.ColSKU: "BRK-001", template.ColCountedQuantity: "4"})
		activePart().expect(d)
		d.inventory.EXPECT().CreateStockCount(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Return(usecase.Document{}, errBoom)
		d.runs.EXPECT().Find(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)

		_, err := uc.Confirm(t.Context(), usecase.Request{
			GarageID: garageID, ActorID: actorID, Type: domain.TypeStockCounts, Content: []byte("xlsx"), Permissions: allPermissions,
		})
		require.Error(t, err)
	})
}

func TestPreviewReferenceLookupFailures(t *testing.T) {
	failures := map[string]func(d *deps){
		"parts by SKU": func(d *deps) {
			d.catalogue.EXPECT().PartsBySKUs(mock.Anything, mock.Anything, mock.Anything).Return(nil, errBoom)
		},
		"units": func(d *deps) {
			d.catalogue.EXPECT().PartsBySKUs(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
			d.catalogue.EXPECT().Units(mock.Anything, mock.Anything).Return(nil, errBoom)
		},
		"barcodes of a receipt": func(d *deps) {
			d.catalogue.EXPECT().PartsBySKUs(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
			d.catalogue.EXPECT().Units(mock.Anything, mock.Anything).Return(nil, nil)
			d.catalogue.EXPECT().PartsByBarcodes(mock.Anything, mock.Anything, mock.Anything).Return(nil, errBoom)
		},
	}

	for name, fail := range failures {
		t.Run(name, func(t *testing.T) {
			uc, d := newDeps(t)
			d.reader.sheet = sheet(domain.TypeGoodsReceipts, map[string]string{
				template.ColSKU: "UNKNOWN", template.ColQuantity: "1", template.ColSupplier: "ACME",
			})
			fail(d)

			_, err := uc.Preview(t.Context(), usecase.Request{
				GarageID: garageID, Type: domain.TypeGoodsReceipts, Content: []byte("xlsx"), Permissions: allPermissions,
			})
			require.Error(t, err)
		})
	}

	t.Run("categories, barcodes and suppliers of a parts file", func(t *testing.T) {
		for name, setup := range map[string]func(d *deps){
			"categories": func(d *deps) {
				d.catalogue.EXPECT().Categories(mock.Anything, mock.Anything).Return(nil, errBoom)
			},
			"barcodes": func(d *deps) {
				d.catalogue.EXPECT().Categories(mock.Anything, mock.Anything).Return(nil, nil)
				d.catalogue.EXPECT().ExistingBarcodes(mock.Anything, mock.Anything, mock.Anything).Return(nil, errBoom)
			},
			"suppliers": func(d *deps) {
				d.catalogue.EXPECT().Categories(mock.Anything, mock.Anything).Return(nil, nil)
				d.catalogue.EXPECT().ExistingBarcodes(mock.Anything, mock.Anything, mock.Anything).Return(map[string]string{}, nil)
				d.suppliers.EXPECT().IDsByName(mock.Anything, mock.Anything, mock.Anything).Return(nil, errBoom)
			},
		} {
			t.Run(name, func(t *testing.T) {
				uc, d := newDeps(t)

				row := partRow()
				row[template.ColBarcodes] = "111"
				row[template.ColOpeningQuantity] = "1"
				row[template.ColSupplier] = "ACME"
				d.reader.sheet = sheet(domain.TypeParts, row)
				d.catalogue.EXPECT().PartsBySKUs(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
				d.catalogue.EXPECT().Units(mock.Anything, mock.Anything).Return(nil, nil)
				setup(d)

				_, err := uc.Preview(t.Context(), usecase.Request{
					GarageID: garageID, Type: domain.TypeParts, Content: []byte("xlsx"), Permissions: allPermissions,
				})
				require.Error(t, err)
			})
		}
	})
}
