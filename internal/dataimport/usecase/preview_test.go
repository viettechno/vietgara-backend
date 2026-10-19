package usecase_test

import (
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/template"
	"github.com/viettechno/vietgara-backend/internal/dataimport/usecase"
)

func TestPreviewPartsValid(t *testing.T) {
	uc, d := newDeps(t)

	row := partRow()
	row[template.ColCategory] = "Brakes"
	row[template.ColBrand] = "Bosch"
	row[template.ColBarcodes] = "1234567890123; BRK-CASE"
	row[template.ColPackagingUnits] = "Case=24"
	row[template.ColOpeningQuantity] = "10"
	row[template.ColUnitCost] = "25000"
	row[template.ColSupplier] = "ACME"

	d.reader.sheet = sheet(domain.TypeParts, row)
	catalogueState{
		units:    []usecase.UnitInfo{{ID: unitID, Name: "Piece", Abbreviation: "pc", IsActive: true}},
		barcodes: map[string]string{},
	}.expect(d)
	expectSuppliers(d, map[string]string{"ACME": supplier})

	p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

	require.True(t, p.CanConfirm)
	require.Equal(t, 1, p.TotalRows)
	require.Equal(t, 1, p.ValidRows)
	require.Empty(t, p.Errors)
	require.Equal(t, []string{
		template.ColSKU, template.ColName, template.ColBaseUnit, template.ColBaseUnitAbbr, template.ColCategory,
		template.ColBrand, template.ColBarcodes, template.ColPackagingUnits, template.ColOpeningQuantity,
		template.ColUnitCost, template.ColSupplier,
	}, p.Columns)
	require.Len(t, p.SampleRows, 1)
	require.Equal(t, 2, p.SampleRows[0].Row)
	require.Equal(t, 1, p.Counts["parts"])
	require.Equal(t, 2, p.Counts["barcodes"])
	require.Equal(t, 1, p.Counts["receipt_lines"])
	require.Equal(t, 1, p.Counts["units"], "the packaging unit Case does not exist yet")
	require.Equal(t, 1, p.Counts["categories"])
}

func TestPreviewUnsupportedType(t *testing.T) {
	uc, _ := newDeps(t)

	_, err := uc.Preview(t.Context(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: domain.Type("nope"), Content: []byte("x"),
		Permissions: allPermissions,
	})
	require.Error(t, err)
}

func TestPreviewEmptyContent(t *testing.T) {
	uc, _ := newDeps(t)

	p := preview(t, uc, domain.TypeParts, nil, allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodeFileEmpty, codeOf(t, p, 0))
	require.Equal(t, 0, p.ValidRows)
}

func TestPreviewFileTooLarge(t *testing.T) {
	uc, _ := newDeps(t)

	p := preview(t, uc, domain.TypeParts, make([]byte, domain.MaxFileBytes+1), allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodeFileTooLarge, codeOf(t, p, 0))
}

func TestPreviewUnreadableFile(t *testing.T) {
	uc, d := newDeps(t)
	d.reader.err = errBoom

	p := preview(t, uc, domain.TypeParts, []byte("not a zip"), allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodeInvalidFile, codeOf(t, p, 0))
}

func TestPreviewEmptySheet(t *testing.T) {
	uc, d := newDeps(t)
	d.reader.sheet = &usecase.Sheet{}

	p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodeFileEmpty, codeOf(t, p, 0))
}

func TestPreviewMissingRequiredColumn(t *testing.T) {
	uc, d := newDeps(t)
	d.reader.sheet = &usecase.Sheet{Rows: []usecase.SheetRow{
		{Number: 1, Cells: []string{template.ColSKU}},
		{Number: 2, Cells: []string{"BRK-001"}},
	}}

	p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodeMissingColumn, codeOf(t, p, 0))
}

func TestPreviewPartsPermission(t *testing.T) {
	uc, d := newDeps(t)
	d.reader.sheet = sheet(domain.TypeParts, partRow())

	p := preview(t, uc, domain.TypeParts, []byte("xlsx"), usecase.Permissions{InventoryWrite: true})

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodePermissionRequired, codeOf(t, p, 0))
	require.Equal(t, 0, p.ValidRows)
}

func TestPreviewPartsColumnPermissions(t *testing.T) {
	tests := map[string]struct {
		mutate map[string]string
		perms  usecase.Permissions
		column string
	}{
		"opening stock needs inventory write": {
			mutate: func() map[string]string {
				row := partRow()
				row[template.ColOpeningQuantity] = "5"

				return row
			}(),
			perms:  usecase.Permissions{CatalogueWrite: true},
			column: template.ColOpeningQuantity,
		},
		"unit cost needs cost read": {
			mutate: func() map[string]string {
				row := partRow()
				row[template.ColUnitCost] = "1000"

				return row
			}(),
			perms:  usecase.Permissions{CatalogueWrite: true},
			column: template.ColUnitCost,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			uc, d := newDeps(t)
			d.reader.sheet = sheet(domain.TypeParts, tt.mutate)

			p := preview(t, uc, domain.TypeParts, []byte("xlsx"), tt.perms)

			require.False(t, p.CanConfirm)
			require.Equal(t, domain.CodePermissionRequired, codeOf(t, p, 0))
			require.Equal(t, tt.column, p.Errors[0].Column)
		})
	}
}

func TestPreviewPartsRowErrors(t *testing.T) {
	tests := map[string]struct {
		mutate map[string]string
		state  catalogueState
		code   string
	}{
		"missing SKU":          {mutate: map[string]string{template.ColName: "n", template.ColBaseUnit: "Piece", template.ColBaseUnitAbbr: "pc"}, code: domain.CodeRequiredValue},
		"missing name":         {mutate: map[string]string{template.ColSKU: "S1", template.ColBaseUnit: "Piece", template.ColBaseUnitAbbr: "pc"}, code: domain.CodeRequiredValue},
		"missing base unit":    {mutate: map[string]string{template.ColSKU: "S1", template.ColName: "n", template.ColBaseUnitAbbr: "pc"}, code: domain.CodeRequiredValue},
		"missing abbreviation": {mutate: map[string]string{template.ColSKU: "S1", template.ColName: "n", template.ColBaseUnit: "Piece"}, code: domain.CodeRequiredValue},
		"invalid barcode":      {mutate: withBarcode("AB@CD"), code: domain.CodeBarcodeInvalid},
		"invalid packaging":    {mutate: withPackaging("Case"), code: domain.CodeInvalidPackaging},
		"base unit packaged":   {mutate: withPackaging("Piece=24"), code: domain.CodeInvalidPackaging},
		"invalid factor":       {mutate: withPackaging("Case=0"), code: domain.CodeInvalidPackaging},
		"opening needs supplier": {
			mutate: withOpening("5", ""), code: domain.CodeSupplierRequired,
		},
		"unknown supplier":     {mutate: withOpening("5", "GHOST"), code: domain.CodeUnknownSupplier},
		"cost without opening": {mutate: withCost("500", "0"), code: domain.CodeInvalidCost},
		"existing SKU": {
			mutate: partRow(), state: catalogueState{parts: []usecase.PartInfo{existingPart("BRK-001")}},
			code: domain.CodeSKUAlreadyExists,
		},
		"existing barcode": {
			mutate: withBarcode("ABC"), state: catalogueState{barcodes: map[string]string{"abc": partID}},
			code: domain.CodeBarcodeAlreadyUsed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			uc, d := newDeps(t)
			d.reader.sheet = sheet(domain.TypeParts, tt.mutate)
			tt.state.expect(d)
			expectSuppliers(d, map[string]string{})

			p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

			require.False(t, p.CanConfirm)
			require.Equal(t, tt.code, codeOf(t, p, 2))
		})
	}
}

func TestPreviewPartsDuplicateSKUAndBarcode(t *testing.T) {
	uc, d := newDeps(t)

	first := withBarcode("SAME")
	first[template.ColSKU] = "BRK-002"
	second := withBarcode("SAME")
	second[template.ColSKU] = "BRK-003"

	d.reader.sheet = sheet(domain.TypeParts, partRow(), partRow(), first, second)
	catalogueState{}.expect(d)
	expectSuppliers(d, nil)

	p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodeSKUDuplicate, codeOf(t, p, 3))
	require.Equal(t, domain.CodeBarcodeAlreadyUsed, codeOf(t, p, 5))
	require.Equal(t, 2, p.ValidRows)
}

func TestPreviewGoodsReceiptsValid(t *testing.T) {
	uc, d := newDeps(t)

	row := map[string]string{
		template.ColReceiptDate: "2026-02-03", template.ColDeliveryNote: "DN-1", template.ColNotes: "n",
		template.ColSKU: "BRK-001", template.ColQuantity: "5", template.ColUnitCost: "1500", template.ColSupplier: "ACME",
	}

	d.reader.sheet = sheet(domain.TypeGoodsReceipts, row)
	catalogueState{
		parts: []usecase.PartInfo{existingPart("BRK-001")},
		units: []usecase.UnitInfo{{ID: unitID, Name: "Piece", Abbreviation: "pc", IsActive: true}},
	}.expect(d)
	expectSuppliers(d, map[string]string{"ACME": supplier})

	p := preview(t, uc, domain.TypeGoodsReceipts, []byte("xlsx"), allPermissions)

	require.True(t, p.CanConfirm)
	require.Equal(t, 1, p.TotalRows)
	require.Equal(t, 1, p.ValidRows)
	require.Equal(t, 1, p.Counts["lines"])
}

func TestPreviewGoodsReceiptsRowErrors(t *testing.T) {
	base := func() map[string]string {
		return map[string]string{
			template.ColSKU: "BRK-001", template.ColQuantity: "5", template.ColSupplier: "ACME",
		}
	}

	tests := map[string]struct {
		mutate map[string]string
		state  catalogueState
		code   string
	}{
		"unknown SKU":      {mutate: base(), state: catalogueState{}, code: domain.CodeUnknownSKU},
		"inactive part":    {mutate: base(), state: catalogueState{parts: []usecase.PartInfo{{ID: partID, SKU: "BRK-001", IsActive: false}}}, code: domain.CodePartInactive},
		"invalid quantity": {mutate: withField(base(), template.ColQuantity, "-1"), state: activePart(), code: domain.CodeInvalidQuantity},
		"missing supplier": {mutate: withField(base(), template.ColSupplier, ""), state: activePart(), code: domain.CodeSupplierRequired},
		"invalid date":     {mutate: withField(base(), template.ColReceiptDate, "31/13/2020"), state: activePart(), code: domain.CodeInvalidDate},
		"unknown unit":     {mutate: withField(base(), template.ColUnit, "Box"), state: activePart(), code: domain.CodeUnknownUnit},
		"unit not for part": {
			mutate: withField(base(), template.ColUnit, "Box"),
			state: func() catalogueState {
				s := activePart()
				s.units = append(s.units, usecase.UnitInfo{ID: catID, Name: "Box", Abbreviation: "box", IsActive: true})

				return s
			}(),
			code: domain.CodeUnitNotForPart,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			uc, d := newDeps(t)
			d.reader.sheet = sheet(domain.TypeGoodsReceipts, tt.mutate)
			tt.state.expect(d)
			expectSuppliers(d, map[string]string{"ACME": supplier})

			p := preview(t, uc, domain.TypeGoodsReceipts, []byte("xlsx"), allPermissions)

			require.False(t, p.CanConfirm)
			require.Equal(t, tt.code, codeOf(t, p, 2))
		})
	}
}

func TestPreviewStockCounts(t *testing.T) {
	uc, d := newDeps(t)

	d.reader.sheet = sheet(domain.TypeStockCounts,
		map[string]string{template.ColName: "Cycle", template.ColNotes: "n", template.ColSKU: "BRK-001", template.ColCountedQuantity: "0"},
		map[string]string{template.ColSKU: "BRK-001", template.ColCountedQuantity: "3"},
		map[string]string{template.ColSKU: "GHOST", template.ColCountedQuantity: "3"},
	)
	activePart().expect(d)

	p := preview(t, uc, domain.TypeStockCounts, []byte("xlsx"), allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodeSKUDuplicate, codeOf(t, p, 3))
	require.Equal(t, domain.CodeUnknownSKU, codeOf(t, p, 4))
	require.Equal(t, 1, p.ValidRows)
	require.Equal(t, 1, p.Counts["lines"])
}

func TestPreviewTruncatesErrors(t *testing.T) {
	uc, d := newDeps(t)

	rows := repeat(domain.MaxErrors+1, func(i int) map[string]string {
		row := partRow()
		row[template.ColSKU] = "S" + itoa(i)
		row[template.ColName] = "" // one error per row

		return row
	})

	d.reader.sheet = sheet(domain.TypeParts, rows...)
	catalogueState{}.expect(d)
	expectSuppliers(d, nil)

	p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

	require.False(t, p.CanConfirm)
	require.True(t, p.ErrorsTruncated)
	require.Len(t, p.Errors, domain.MaxErrors)
	require.Equal(t, domain.MaxErrors+1, p.TotalRows)
}

func TestPreviewTooManyRows(t *testing.T) {
	uc, d := newDeps(t)

	rows := repeat(domain.MaxRows+1, func(i int) map[string]string {
		row := partRow()
		row[template.ColSKU] = "S" + itoa(i)

		return row
	})

	d.reader.sheet = sheet(domain.TypeParts, rows...)
	catalogueState{}.expect(d)

	p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.MaxRows+1, p.TotalRows)
	require.Equal(t, domain.CodeTooManyRows, codeOf(t, p, 0))
}

func TestPreviewFiveHundredRowsAccepted(t *testing.T) {
	uc, d := newDeps(t)

	rows := repeat(500, func(i int) map[string]string {
		row := partRow()
		row[template.ColSKU] = "S" + itoa(i)

		return row
	})

	d.reader.sheet = sheet(domain.TypeParts, rows...)
	catalogueState{}.expect(d)

	p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

	require.True(t, p.CanConfirm)
	require.Equal(t, 500, p.TotalRows)
	require.Equal(t, 500, p.ValidRows)
	require.Len(t, p.SampleRows, 20)
}

func TestPreviewFiveThousandRowsPerforms(t *testing.T) {
	uc, d := newDeps(t)

	rows := repeat(domain.MaxRows, func(i int) map[string]string {
		row := partRow()
		row[template.ColSKU] = "S" + itoa(i)

		return row
	})

	d.reader.sheet = sheet(domain.TypeParts, rows...)
	catalogueState{}.expect(d)

	start := time.Now()
	p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)
	require.Less(t, time.Since(start), 10*time.Second)

	require.True(t, p.CanConfirm)
	require.Equal(t, domain.MaxRows, p.ValidRows)
}

// activePart is a catalogue state with one active part and its base unit.
func activePart() catalogueState {
	return catalogueState{
		parts: []usecase.PartInfo{existingPart("BRK-001")},
		units: []usecase.UnitInfo{{ID: unitID, Name: "Piece", Abbreviation: "pc", IsActive: true}},
	}
}

func withBarcode(code string) map[string]string {
	row := partRow()
	row[template.ColBarcodes] = code

	return row
}

func withPackaging(pack string) map[string]string {
	row := partRow()
	row[template.ColPackagingUnits] = pack

	return row
}

func withOpening(qty, supplierName string) map[string]string {
	row := partRow()
	row[template.ColOpeningQuantity] = qty
	row[template.ColSupplier] = supplierName

	return row
}

func withCost(cost, opening string) map[string]string {
	row := partRow()
	row[template.ColUnitCost] = cost
	row[template.ColOpeningQuantity] = opening

	return row
}

func withField(row map[string]string, header, value string) map[string]string {
	out := make(map[string]string, len(row))
	maps.Copy(out, row)

	out[header] = value

	return out
}

func itoa(i int) string {
	const digits = "0123456789"

	if i == 0 {
		return "0"
	}

	var buf []byte
	for i > 0 {
		buf = append([]byte{digits[i%10]}, buf...)
		i /= 10
	}

	return string(buf)
}

func TestPreviewPartsPackagingBarcodes(t *testing.T) {
	tests := map[string]struct {
		barcodes string
		code     string
	}{
		"base and packaging codes": {barcodes: "111; 222@Case"},
		"unit not on the row":      {barcodes: "222@Box", code: domain.CodeBarcodeInvalid},
		"unit without a code":      {barcodes: "@Case", code: domain.CodeBarcodeInvalid},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			uc, d := newDeps(t)

			row := partRow()
			row[template.ColPackagingUnits] = "Case=24"
			row[template.ColBarcodes] = tt.barcodes

			d.reader.sheet = sheet(domain.TypeParts, row)
			catalogueState{units: []usecase.UnitInfo{{ID: unitID, Name: "Piece", Abbreviation: "pc", IsActive: true}}}.expect(d)
			expectSuppliers(d, nil)

			p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

			require.Equal(t, tt.code == "", p.CanConfirm)
			require.Equal(t, tt.code, codeOf(t, p, 2))
		})
	}
}

// scannedCase is a part with a case packaging unit and the barcode of the case.
func scannedCase() catalogueState {
	part := existingPart("BRK-001")
	part.PackagingUnits = []usecase.PackagingUnit{{UnitID: catID, Factor: "24"}}

	return catalogueState{
		units: []usecase.UnitInfo{
			{ID: unitID, Name: "Piece", Abbreviation: "pc", IsActive: true},
			{ID: catID, Name: "Case", Abbreviation: "cs", IsActive: true},
		},
		scanned: map[string]usecase.BarcodeMatch{
			"89300": {Part: part, UnitID: catID},
			"89301": {Part: part},
		},
	}
}

func TestPreviewReceiptResolvesBarcodeAndItsUnit(t *testing.T) {
	tests := map[string]struct {
		code string
		unit string
		want string
	}{
		"case barcode":                {code: "89300"},
		"case barcode, same unit":     {code: "89300", unit: "cs"},
		"case barcode, base unit":     {code: "89300", unit: "Piece", want: domain.CodeUnitMismatch},
		"base barcode, case unit":     {code: "89301", unit: "Case"},
		"unknown code":                {code: "00000", want: domain.CodeUnknownSKU},
		"case-insensitive code match": {code: "89300"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			uc, d := newDeps(t)

			d.reader.sheet = sheet(domain.TypeGoodsReceipts, map[string]string{
				template.ColSKU: tt.code, template.ColQuantity: "2", template.ColSupplier: "ACME", template.ColUnit: tt.unit,
			})
			scannedCase().expect(d)
			expectSuppliers(d, map[string]string{"ACME": supplier})

			p := preview(t, uc, domain.TypeGoodsReceipts, []byte("xlsx"), allPermissions)

			require.Equal(t, tt.want == "", p.CanConfirm, p.Errors)
			require.Equal(t, tt.want, codeOf(t, p, 2))
		})
	}
}

func TestPreviewCountResolvesBarcode(t *testing.T) {
	uc, d := newDeps(t)

	d.reader.sheet = sheet(domain.TypeStockCounts,
		map[string]string{template.ColSKU: "89300", template.ColCountedQuantity: "3"},
		map[string]string{template.ColSKU: "BRK-001", template.ColCountedQuantity: "3"},
	)
	state := scannedCase()
	state.parts = []usecase.PartInfo{existingPart("BRK-001")}
	state.expect(d)

	p := preview(t, uc, domain.TypeStockCounts, []byte("xlsx"), allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodeSKUDuplicate, codeOf(t, p, 3), "the barcode and the SKU name the same part")
}

func TestPreviewReceiptHeaderMustAgree(t *testing.T) {
	line := func(note, date string) map[string]string {
		return map[string]string{
			template.ColSKU: "BRK-001", template.ColQuantity: "1", template.ColSupplier: "ACME",
			template.ColDeliveryNote: note, template.ColReceiptDate: date,
		}
	}

	uc, d := newDeps(t)
	d.reader.sheet = sheet(domain.TypeGoodsReceipts,
		line("DN-1", ""), line("", "2026-02-03"), line("DN-1", "2026-02-03"), line("DN-2", "2026-02-04"),
	)
	activePart().expect(d)
	expectSuppliers(d, map[string]string{"ACME": supplier})

	p := preview(t, uc, domain.TypeGoodsReceipts, []byte("xlsx"), allPermissions)

	require.False(t, p.CanConfirm)
	require.Empty(t, codeOf(t, p, 2))
	require.Empty(t, codeOf(t, p, 3))
	require.Empty(t, codeOf(t, p, 4))
	require.Equal(t, domain.CodeHeaderMismatch, codeOf(t, p, 5))
}

func TestPreviewCountHeaderMustAgree(t *testing.T) {
	uc, d := newDeps(t)

	d.reader.sheet = sheet(domain.TypeStockCounts,
		map[string]string{template.ColName: "Shelf A", template.ColSKU: "BRK-001", template.ColCountedQuantity: "1"},
		map[string]string{template.ColName: "Shelf B", template.ColSKU: "BRK-002", template.ColCountedQuantity: "1"},
	)
	catalogueState{parts: []usecase.PartInfo{existingPart("BRK-001"), existingPart("BRK-002")}}.expect(d)

	p := preview(t, uc, domain.TypeStockCounts, []byte("xlsx"), allPermissions)

	require.False(t, p.CanConfirm)
	require.Equal(t, domain.CodeHeaderMismatch, codeOf(t, p, 3))
}

func TestPreviewPartsRefusesWhatTheCatalogueWould(t *testing.T) {
	inactive := usecase.UnitInfo{ID: catID, Name: "Case", Abbreviation: "cs", IsActive: false}
	units := []usecase.UnitInfo{{ID: unitID, Name: "Piece", Abbreviation: "pc", IsActive: true}, inactive}

	t.Run("an inactive base unit", func(t *testing.T) {
		uc, d := newDeps(t)

		row := partRow()
		row[template.ColBaseUnit] = "Case"
		d.reader.sheet = sheet(domain.TypeParts, row)
		catalogueState{units: units}.expect(d)
		expectSuppliers(d, nil)

		p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

		require.False(t, p.CanConfirm)
		require.Equal(t, domain.CodeInvalidUnit, codeOf(t, p, 2))
	})

	t.Run("an inactive packaging unit", func(t *testing.T) {
		uc, d := newDeps(t)

		row := partRow()
		row[template.ColPackagingUnits] = "Case=12"
		d.reader.sheet = sheet(domain.TypeParts, row)
		catalogueState{units: units}.expect(d)
		expectSuppliers(d, nil)

		p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

		require.False(t, p.CanConfirm)
		require.Equal(t, domain.CodeInvalidUnit, codeOf(t, p, 2))
	})

	t.Run("more barcodes than a part can hold", func(t *testing.T) {
		uc, d := newDeps(t)

		codes := make([]string, 0, domain.MaxBarcodesPerPart+1)
		for i := range domain.MaxBarcodesPerPart + 1 {
			codes = append(codes, fmt.Sprintf("89%04d", i))
		}

		row := partRow()
		row[template.ColBarcodes] = strings.Join(codes, ";")
		d.reader.sheet = sheet(domain.TypeParts, row)
		catalogueState{units: units}.expect(d)
		expectSuppliers(d, nil)

		p := preview(t, uc, domain.TypeParts, []byte("xlsx"), allPermissions)

		require.False(t, p.CanConfirm)
		require.Equal(t, domain.CodeBarcodeInvalid, codeOf(t, p, 2))
	})
}
