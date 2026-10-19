package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/template"
	"github.com/viettechno/vietgara-backend/internal/dataimport/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

var errBoom = errors.New("boom")

const (
	garageID = "11111111-1111-1111-1111-111111111111"
	actorID  = "22222222-2222-2222-2222-222222222222"
	partID   = "33333333-3333-3333-3333-333333333333"
	unitID   = "44444444-4444-4444-4444-444444444444"
	catID    = "55555555-5555-5555-5555-555555555555"
	supplier = "77777777-7777-7777-7777-777777777777"
)

// allPermissions allows every type and every column.
var allPermissions = usecase.Permissions{
	CatalogueWrite: true, InventoryWrite: true, InventoryCostRead: true, InventoryCount: true,
}

type deps struct {
	catalogue *mocks.MockImportCatalogue
	suppliers *mocks.MockImportSuppliers
	inventory *mocks.MockImportInventory
	runs      *mocks.MockImportRunRepository
	audit     *mocks.MockImportAuditLogger
	reader    *fakeReader
}

func newDeps(t *testing.T) (*usecase.UseCase, *deps) {
	t.Helper()

	d := &deps{
		catalogue: mocks.NewMockImportCatalogue(t), suppliers: mocks.NewMockImportSuppliers(t),
		inventory: mocks.NewMockImportInventory(t), runs: mocks.NewMockImportRunRepository(t),
		audit: mocks.NewMockImportAuditLogger(t), reader: &fakeReader{},
	}

	uc := usecase.NewUseCase(usecase.Dependencies{
		Catalogue: d.catalogue, Suppliers: d.suppliers, Inventory: d.inventory, Runs: d.runs, Reader: d.reader, Audit: d.audit,
	})

	return uc, d
}

// fakeReader returns a canned sheet (or error) regardless of the payload.
type fakeReader struct {
	sheet *usecase.Sheet
	err   error
}

func (f *fakeReader) Read(_ []byte) (*usecase.Sheet, error) {
	return f.sheet, f.err
}

// sheet builds a workbook from header/value maps in the template's column
// order. The header row is row 1, so data rows start at 2.
func sheet(typ domain.Type, rows ...map[string]string) *usecase.Sheet {
	headers := template.Columns(typ)
	out := &usecase.Sheet{Rows: []usecase.SheetRow{{Number: 1, Cells: headers}}}

	for i, row := range rows {
		cells := make([]string, len(headers))
		for c, header := range headers {
			cells[c] = row[header]
		}

		out.Rows = append(out.Rows, usecase.SheetRow{Number: i + 2, Cells: cells})
	}

	return out
}

// repeat builds n row maps with build.
func repeat(n int, build func(i int) map[string]string) []map[string]string {
	out := make([]map[string]string, 0, n)
	for i := range n {
		out = append(out, build(i))
	}

	return out
}

func partRow() map[string]string {
	return map[string]string{
		template.ColSKU: "BRK-001", template.ColName: "Brake pad", template.ColBaseUnit: "Piece",
		template.ColBaseUnitAbbr: "pc",
	}
}

// catalogueState wires the reference lookups every preview performs. Missing
// maps mean "nothing exists yet".
type catalogueState struct {
	parts      []usecase.PartInfo
	units      []usecase.UnitInfo
	categories []usecase.CategoryInfo
	barcodes   map[string]string
	// scanned maps a lower-case barcode to the part it identifies.
	scanned map[string]usecase.BarcodeMatch
}

func (s catalogueState) expect(d *deps) {
	d.catalogue.EXPECT().PartsBySKUs(mock.Anything, mock.Anything, mock.Anything).Return(s.parts, nil).Maybe()
	d.catalogue.EXPECT().Units(mock.Anything, mock.Anything).Return(s.units, nil).Maybe()
	d.catalogue.EXPECT().Categories(mock.Anything, mock.Anything).Return(s.categories, nil).Maybe()
	d.catalogue.EXPECT().PartsByBarcodes(mock.Anything, mock.Anything, mock.Anything).
		Return(nonNilMatches(s.scanned), nil).Maybe()
	d.catalogue.EXPECT().ExistingBarcodes(mock.Anything, mock.Anything, mock.Anything).
		Return(nonNilMap(s.barcodes), nil).Maybe()
}

func nonNilMatches(in map[string]usecase.BarcodeMatch) map[string]usecase.BarcodeMatch {
	if in == nil {
		return map[string]usecase.BarcodeMatch{}
	}

	return in
}

func nonNilMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}

	return in
}

func expectSuppliers(d *deps, ids map[string]string) {
	d.suppliers.EXPECT().IDsByName(mock.Anything, mock.Anything, mock.Anything).Return(nonNilMap(ids), nil).Maybe()
}

func existingPart(sku string) usecase.PartInfo {
	return usecase.PartInfo{ID: partID, SKU: sku, Name: "Brake pad", IsActive: true, BaseUnitID: unitID}
}

func preview(t *testing.T, uc *usecase.UseCase, typ domain.Type, content []byte, perms usecase.Permissions) *domain.Preview {
	t.Helper()

	p, err := uc.Preview(context.Background(), usecase.Request{
		GarageID: garageID, ActorID: actorID, Type: typ, FileName: "file.xlsx", Content: content, Permissions: perms,
	})
	require.NoError(t, err)

	return p
}

// codeOf returns the code of the first error on the given row.
func codeOf(t *testing.T, p *domain.Preview, row int) string {
	t.Helper()

	for _, e := range p.Errors {
		if e.Row == row {
			return e.Code
		}
	}

	return ""
}

// reasonOf extracts the stable reason of an *apperror.AppError or of a gRPC
// status-carried error.
func reasonOf(t *testing.T, err error) string {
	t.Helper()

	require.Error(t, err)

	if appErr, ok := apperror.As(err); ok {
		return appErr.Reason
	}

	st, ok := status.FromError(err)
	require.True(t, ok, "expected an app error, got %T: %v", err, err)

	for _, detail := range st.Details() {
		if info, ok := detail.(interface{ GetReason() string }); ok {
			return info.GetReason()
		}
	}

	return ""
}
