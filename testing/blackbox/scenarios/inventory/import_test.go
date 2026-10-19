// Scenario: E2E-INV-033 to E2E-INV-038
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/07_Inventory.md
package inventory_test

import (
	"bytes"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// The template headers of each import type (API Specification Section 8.10).
var (
	partsColumns    = []string{"SKU", "Name", "Base unit", "Base unit abbreviation", "Category", "Brand", "Barcodes", "Packaging units", "Opening quantity", "Unit cost", "Supplier"}
	receiptsColumns = []string{"Receipt date", "Delivery note", "Notes", "SKU", "Quantity", "Unit", "Unit cost", "Supplier"}
	countsColumns   = []string{"Name", "Notes", "SKU", "Counted quantity", "Unit"}
)

const (
	typeParts    = inventoryv1.ImportType_IMPORT_TYPE_PARTS
	typeReceipts = inventoryv1.ImportType_IMPORT_TYPE_GOODS_RECEIPTS
	typeCounts   = inventoryv1.ImportType_IMPORT_TYPE_STOCK_COUNTS
)

type row = map[string]string

// workbook renders rows under headers into an .xlsx, every cell as text.
func workbook(t *testing.T, headers []string, rows []row) []byte {
	t.Helper()

	f := excelize.NewFile()

	defer func() { _ = f.Close() }()

	head := make([]any, len(headers))
	for i, h := range headers {
		head[i] = h
	}

	require.NoError(t, f.SetSheetRow("Sheet1", "A1", &head))

	for i, r := range rows {
		cells := make([]any, len(headers))
		for c, h := range headers {
			cells[c] = r[h]
		}

		cell, err := excelize.CoordinatesToCellName(1, i+2)
		require.NoError(t, err)
		require.NoError(t, f.SetSheetRow("Sheet1", cell, &cells))
	}

	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))

	return buf.Bytes()
}

// partsRows is a parts file of n parts: a barcode on each, every tenth with a
// Case of 12 and its own barcode, every one but each seventh with opening
// stock from supplierName.
func partsRows(n int, supplierName string) []row { return taggedPartsRows(n, supplierName, "") }

// taggedPartsRows is partsRows with tag in every SKU and barcode, so a second
// file in the same garage does not repeat the first one's.
func taggedPartsRows(n int, supplierName, tag string) []row {
	rows := make([]row, 0, n)

	for i := range n {
		r := row{
			"SKU": fmt.Sprintf("P%s-%04d", tag, i), "Name": fmt.Sprintf("Part %d", i), "Base unit": "Piece",
			"Base unit abbreviation": "pc", "Category": "Filters", "Brand": "Bosch", "Barcodes": fmt.Sprintf("89%s%06d", tag, i),
		}

		if i%10 == 0 {
			r["Packaging units"] = "Case=12"
			r["Barcodes"] += fmt.Sprintf(";79%s%06d@Case", tag, i)
		}

		if i%7 != 0 {
			r["Opening quantity"] = strconv.Itoa(i%5 + 1)
			r["Unit cost"] = strconv.Itoa(1000 + i)
			r["Supplier"] = supplierName
		}

		rows = append(rows, r)
	}

	return rows
}

func cloneRows(rows []row) []row {
	out := make([]row, len(rows))
	for i, r := range rows {
		out[i] = maps.Clone(r)
	}

	return out
}

// problems returns the preview's errors keyed "row:column" with their codes.
func problems(preview *inventoryv1.ImportPreview) map[string]string {
	out := map[string]string{}
	for _, e := range preview.GetErrors() {
		out[fmt.Sprintf("%d:%s", e.GetRow(), e.GetColumn())] = e.GetCode()
	}

	return out
}

// Scenario: E2E-INV-033, Release 7.2: a parts file with five bad rows lists
// all five and saves nothing, not even a unit; the fixed file creates the
// parts, units, barcodes (also a case barcode) and the opening stock as one
// completed goods receipt with a supplier on each line.
func TestImportPartsAllOrNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-import-parts")
	garageID := garage.GetId()
	acme := supplier(t, owner.API, garageID, "ACME ")

	good := partsRows(500, acme.GetName())
	bad := cloneRows(good)
	bad[3]["Name"] = ""                  // row 5
	bad[20]["SKU"] = bad[19]["SKU"]      // row 22
	bad[40]["Supplier"] = "Nobody"       // row 42
	bad[60]["Barcodes"] = "bad_code"     // row 62
	bad[81]["Opening quantity"] = "many" // row 83
	badFile := workbook(t, partsColumns, bad)

	t.Run("the preview lists every problem and the confirm writes nothing", func(t *testing.T) {
		preview, err := owner.API.PreviewImport(ctx, garageID, typeParts, "parts.xlsx", badFile)
		require.NoError(t, err)

		assert.False(t, preview.GetCanConfirm())
		assert.EqualValues(t, 500, preview.GetTotalRows())
		assert.EqualValues(t, 495, preview.GetValidRows())
		assert.Equal(t, map[string]string{
			"5:Name": "REQUIRED_VALUE_MISSING", "22:SKU": "SKU_DUPLICATE", "42:Supplier": "UNKNOWN_SUPPLIER",
			"62:Barcodes": "BARCODE_INVALID", "83:Opening quantity": "INVALID_QUANTITY",
		}, problems(preview))

		_, err = owner.API.ConfirmImport(ctx, garageID, typeParts, "parts.xlsx", badFile)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "IMPORT_FILE_INVALID")

		parts, err := owner.API.ListParts(ctx, garageID, url.Values{"pageSize": {"100"}})
		require.NoError(t, err)
		assert.Empty(t, parts)

		units, err := owner.API.ListUnits(ctx, garageID, true)
		require.NoError(t, err)
		assert.Empty(t, units, "not even the units of the good rows")
	})

	goodFile := workbook(t, partsColumns, good)

	t.Run("the fixed file imports the parts, units, barcodes and opening stock", func(t *testing.T) {
		preview, err := owner.API.PreviewImport(ctx, garageID, typeParts, "parts.xlsx", goodFile)
		require.NoError(t, err)
		require.True(t, preview.GetCanConfirm(), "%v", preview.GetErrors())
		assert.Len(t, preview.GetSampleRows(), 20)
		assert.EqualValues(t, 500, preview.GetCounts()["parts"])
		assert.EqualValues(t, 1, preview.GetCounts()["categories"])
		assert.EqualValues(t, 2, preview.GetCounts()["units"])
		assert.EqualValues(t, 550, preview.GetCounts()["barcodes"])
		assert.EqualValues(t, 428, preview.GetCounts()["receipt_lines"])

		result, err := owner.API.ConfirmImport(ctx, garageID, typeParts, "parts.xlsx", goodFile)
		require.NoError(t, err)
		assert.EqualValues(t, 500, result.GetRowsImported())
		require.NotEmpty(t, result.GetDocumentId())

		receipt, err := owner.API.GetGoodsReceipt(ctx, garageID, result.GetDocumentId())
		require.NoError(t, err)
		assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_COMPLETED, receipt.GetStatus())
		require.Len(t, receipt.GetLines(), 428)

		for _, line := range receipt.GetLines() {
			assert.Equal(t, acme.GetId(), line.GetSupplierId(), "every line carries its supplier")
		}

		units, err := owner.API.ListUnits(ctx, garageID, true)
		require.NoError(t, err)
		assert.Len(t, units, 2, "Piece and Case")

		categories, err := owner.API.ListPartCategories(ctx, garageID, true)
		require.NoError(t, err)
		require.Len(t, categories, 1)
		assert.Equal(t, "Filters", categories[0].GetName())

		withCase, err := owner.API.SearchParts(ctx, garageID, "P-0010")
		require.NoError(t, err)
		require.Len(t, withCase, 1)
		require.Len(t, withCase[0].GetPackagingUnits(), 1)
		assert.Equal(t, "12", withCase[0].GetPackagingUnits()[0].GetFactor())
		require.Len(t, withCase[0].GetBarcodes(), 2)

		caseCode := ""

		for _, b := range withCase[0].GetBarcodes() {
			if b.GetUnitId() != "" {
				caseCode = b.GetBarcode()
			}
		}

		assert.Equal(t, "79000010", caseCode, "the case barcode names the Case unit")

		found, err := owner.API.LookupPartByBarcode(ctx, garageID, caseCode)
		require.NoError(t, err)
		assert.Equal(t, withCase[0].GetId(), found.GetPart().GetId())
		assert.NotEmpty(t, found.GetBarcode().GetUnitId())

		// P-0003 is received with 3%5+1 = 4; P-0000 has no opening stock.
		stocked, err := owner.API.SearchParts(ctx, garageID, "P-0003")
		require.NoError(t, err)
		require.Len(t, stocked, 1)

		level, err := owner.API.GetStockLevel(ctx, garageID, stocked[0].GetId())
		require.NoError(t, err)
		assert.Equal(t, "4", level.GetQuantityOnHand())
	})

	t.Run("a SKU or barcode that exists is an error, the import never updates", func(t *testing.T) {
		preview, err := owner.API.PreviewImport(ctx, garageID, typeParts, "again.xlsx", workbook(t, partsColumns, good[:3]))
		require.NoError(t, err)

		assert.False(t, preview.GetCanConfirm())
		assert.Equal(t, "SKU_ALREADY_EXISTS", problems(preview)["2:SKU"])
		assert.Equal(t, "BARCODE_ALREADY_USED", problems(preview)["2:Barcodes"])
	})
}

// Scenario: E2E-INV-034, Release 7.2: a confirmed file cannot be confirmed
// again; changing it makes a new file; two confirmations of one file import
// once.
func TestImportIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-import-idem")
	garageID := garage.GetId()
	acme := supplier(t, owner.API, garageID, "ACME ")
	piece := fixture.NewUnit(t, owner.API, garageID, "Piece", "pc")
	fixture.NewPart(t, owner.API, garageID, "FILTER-01", "Oil filter", piece)

	file := func(quantity string) []byte {
		return workbook(t, receiptsColumns, []row{
			{"SKU": "FILTER-01", "Quantity": quantity, "Supplier": acme.GetName()},
			{"SKU": "FILTER-01", "Quantity": "1", "Supplier": acme.GetName()},
		})
	}

	first, err := owner.API.ConfirmImport(ctx, garageID, typeReceipts, "gr.xlsx", file("5"))
	require.NoError(t, err)
	require.NotEmpty(t, first.GetDocumentId())

	_, err = owner.API.ConfirmImport(ctx, garageID, typeReceipts, "gr.xlsx", file("5"))
	fixture.RequireAPIError(t, err, http.StatusConflict, "IMPORT_FILE_ALREADY_IMPORTED")

	receipts, err := owner.API.ListGoodsReceipts(ctx, garageID, url.Values{"pageSize": {"100"}})
	require.NoError(t, err)
	assert.Len(t, receipts, 1, "the repeat created nothing")

	_, err = owner.API.ConfirmImport(ctx, garageID, typeReceipts, "gr.xlsx", file("6"))
	require.NoError(t, err)

	// A cancelled draft does not free the file: change it to import again.
	_, err = owner.API.CancelGoodsReceipt(ctx, garageID, first.GetDocumentId())
	require.NoError(t, err)

	_, err = owner.API.ConfirmImport(ctx, garageID, typeReceipts, "gr.xlsx", file("5"))
	fixture.RequireAPIError(t, err, http.StatusConflict, "IMPORT_FILE_ALREADY_IMPORTED")

	// Two confirmations of one new file at the same time import it once.
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		errs   []error
		oks    int
		racing = file("7")
	)

	for range 2 {
		wg.Go(func() {
			_, err := owner.API.ConfirmImport(ctx, garageID, typeReceipts, "race.xlsx", racing)

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				oks++
			} else {
				errs = append(errs, err)
			}
		})
	}

	wg.Wait()

	assert.Equal(t, 1, oks, "exactly one confirmation wins")
	require.Len(t, errs, 1)
	fixture.RequireAPIError(t, errs[0], http.StatusConflict, "IMPORT_FILE_ALREADY_IMPORTED")

	receipts, err = owner.API.ListGoodsReceipts(ctx, garageID, url.Values{"pageSize": {"100"}})
	require.NoError(t, err)
	assert.Len(t, receipts, 3, "first, changed and the raced file")
}

// Scenario: E2E-INV-035, Release 7.2: a goods receipt file makes one draft;
// lines are found by SKU or barcode, a case barcode counts in cartons, each
// line has its supplier, and the header columns must agree.
func TestImportGoodsReceipt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-import-gr")
	garageID := garage.GetId()
	acme, bolt := supplier(t, owner.API, garageID, "ACME "), supplier(t, owner.API, garageID, "Bolt ")

	can := fixture.NewUnit(t, owner.API, garageID, "Can", "can")
	carton := fixture.NewUnit(t, owner.API, garageID, "Carton", "ctn")
	beer := fixture.NewPart(t, owner.API, garageID, "BEER-330", "Beer", can)
	old := fixture.NewPart(t, owner.API, garageID, "OLD-1", "Old part", can)

	_, err := owner.API.SetPartUnits(ctx, garageID, beer.GetId(), []*cataloguev1.PackagingUnit{{UnitId: carton.GetId(), Factor: "24"}})
	require.NoError(t, err)

	_, err = owner.API.SetPartBarcodes(ctx, garageID, beer.GetId(), []*cataloguev1.PartBarcode{
		{Barcode: "8930001"}, {Barcode: "8930024", UnitId: new(carton.GetId())},
	})
	require.NoError(t, err)

	_, err = owner.API.DeletePart(ctx, garageID, old.GetId(), false)
	require.NoError(t, err)

	t.Run("one draft, lines by SKU and barcode, a supplier on each line", func(t *testing.T) {
		file := workbook(t, receiptsColumns, []row{
			{"SKU": "8930024", "Quantity": "2", "Supplier": acme.GetName(), "Unit cost": "12000", "Delivery note": "DN-77", "Receipt date": "2026-10-01"},
			{"SKU": "BEER-330", "Quantity": "5", "Supplier": bolt.GetName()},
			{"SKU": "8930001", "Quantity": "1", "Supplier": bolt.GetName(), "Unit": "can"},
		})

		preview, err := owner.API.PreviewImport(ctx, garageID, typeReceipts, "gr.xlsx", file)
		require.NoError(t, err)
		require.True(t, preview.GetCanConfirm(), "%v", preview.GetErrors())

		result, err := owner.API.ConfirmImport(ctx, garageID, typeReceipts, "gr.xlsx", file)
		require.NoError(t, err)

		draft, err := owner.API.GetGoodsReceipt(ctx, garageID, result.GetDocumentId())
		require.NoError(t, err)
		assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_DRAFT, draft.GetStatus())
		assert.Equal(t, "DN-77", draft.GetDeliveryNoteNumber())
		assert.Equal(t, "2026-10-01", draft.GetReceiptDate())
		require.Len(t, draft.GetLines(), 3)

		assert.Equal(t, carton.GetId(), draft.GetLines()[0].GetUnitId(), "the case barcode implies its unit")
		assert.Equal(t, "2", draft.GetLines()[0].GetEnteredQuantity())
		assert.Equal(t, "48", draft.GetLines()[0].GetQuantity())
		assert.Equal(t, acme.GetId(), draft.GetLines()[0].GetSupplierId())
		assert.Equal(t, bolt.GetId(), draft.GetLines()[1].GetSupplierId())

		level, err := owner.API.GetStockLevel(ctx, garageID, beer.GetId())
		require.NoError(t, err)
		assert.Equal(t, "0", level.GetQuantityOnHand(), "a draft changes nothing")

		_, err = owner.API.CompleteGoodsReceipt(ctx, garageID, draft.GetId())
		require.NoError(t, err)

		level, err = owner.API.GetStockLevel(ctx, garageID, beer.GetId())
		require.NoError(t, err)
		assert.Equal(t, "54", level.GetQuantityOnHand(), "48 + 5 + 1")
	})

	t.Run("every problem is listed", func(t *testing.T) {
		file := workbook(t, receiptsColumns, []row{
			{"SKU": "BEER-330", "Quantity": "1", "Supplier": acme.GetName(), "Delivery note": "DN-1"},
			{"SKU": "BEER-330", "Quantity": "1", "Supplier": "Nobody", "Delivery note": "DN-2"},
			{"SKU": "OLD-1", "Quantity": "1", "Supplier": acme.GetName()},
			{"SKU": "8930024", "Quantity": "1", "Supplier": acme.GetName(), "Unit": "can"},
			{"SKU": "NO-SUCH", "Quantity": "0", "Supplier": acme.GetName()},
		})

		preview, err := owner.API.PreviewImport(ctx, garageID, typeReceipts, "bad.xlsx", file)
		require.NoError(t, err)

		assert.False(t, preview.GetCanConfirm())

		got := problems(preview)
		assert.Equal(t, "HEADER_MISMATCH", got["3:Delivery note"])
		assert.Equal(t, "UNKNOWN_SUPPLIER", got["3:Supplier"])
		assert.Equal(t, "UNKNOWN_SKU", got["4:SKU"], "a deleted part is not found")
		assert.Equal(t, "UNIT_MISMATCH", got["5:Unit"])
		assert.Equal(t, "UNKNOWN_SKU", got["6:SKU"])
		assert.Equal(t, "INVALID_QUANTITY", got["6:Quantity"])
	})
}

// Scenario: E2E-INV-036, Release 7.2: a stock count file makes one draft; a
// case barcode counts in cartons; zero is a real count; a part twice or a
// unit that is not the code's is refused.
func TestImportStockCount(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-import-count")
	garageID := garage.GetId()

	can := fixture.NewUnit(t, owner.API, garageID, "Can", "can")
	carton := fixture.NewUnit(t, owner.API, garageID, "Carton", "ctn")
	beer := fixture.NewPart(t, owner.API, garageID, "BEER-330", "Beer", can)
	filter := fixture.NewPart(t, owner.API, garageID, "FILTER-01", "Filter", can)

	_, err := owner.API.SetPartUnits(ctx, garageID, beer.GetId(), []*cataloguev1.PackagingUnit{{UnitId: carton.GetId(), Factor: "24"}})
	require.NoError(t, err)

	_, err = owner.API.SetPartBarcodes(ctx, garageID, beer.GetId(), []*cataloguev1.PartBarcode{
		{Barcode: "8930001"}, {Barcode: "8930024", UnitId: new(carton.GetId())},
	})
	require.NoError(t, err)

	_, err = owner.API.ReceiveStock(ctx, garageID, beer.GetId(), "100", "", 1000)
	require.NoError(t, err)

	_, err = owner.API.ReceiveStock(ctx, garageID, filter.GetId(), "20", "", 1000)
	require.NoError(t, err)

	file := workbook(t, countsColumns, []row{
		{"Name": "Shelf A", "SKU": "8930024", "Counted quantity": "3"},
		{"SKU": "FILTER-01", "Counted quantity": "0"},
	})

	result, err := owner.API.ConfirmImport(ctx, garageID, typeCounts, "count.xlsx", file)
	require.NoError(t, err)

	count, err := owner.API.GetStockCount(ctx, garageID, result.GetDocumentId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_DRAFT, count.GetStatus())
	assert.Equal(t, "Shelf A", count.GetName())
	require.Len(t, count.GetLines(), 2)

	bySKU := map[string]*inventoryv1.StockCountLine{}
	for _, l := range count.GetLines() {
		bySKU[l.GetPartSku()] = l
	}

	assert.Equal(t, carton.GetId(), bySKU["BEER-330"].GetUnitId())
	assert.Equal(t, "3", bySKU["BEER-330"].GetEnteredCountedQuantity())
	assert.Equal(t, "72", bySKU["BEER-330"].GetCountedQuantity(), "3 cartons of 24")
	assert.Equal(t, "100", bySKU["BEER-330"].GetSystemQuantity())
	assert.Equal(t, "-28", bySKU["BEER-330"].GetDifferenceQuantity())
	assert.Equal(t, "0", bySKU["FILTER-01"].GetCountedQuantity(), "zero is a real count of none")
	assert.Equal(t, "-20", bySKU["FILTER-01"].GetDifferenceQuantity())

	level, err := owner.API.GetStockLevel(ctx, garageID, beer.GetId())
	require.NoError(t, err)
	assert.Equal(t, "100", level.GetQuantityOnHand(), "nothing is posted until the count is completed")

	bad := workbook(t, countsColumns, []row{
		{"SKU": "BEER-330", "Counted quantity": "1"},
		{"SKU": "8930001", "Counted quantity": "1"},
		{"SKU": "8930024", "Counted quantity": "1", "Unit": "can"},
	})

	preview, err := owner.API.PreviewImport(ctx, garageID, typeCounts, "bad.xlsx", bad)
	require.NoError(t, err)

	got := problems(preview)
	assert.Equal(t, "SKU_DUPLICATE", got["3:SKU"], "the SKU and the barcode name the same part")
	assert.Equal(t, "UNIT_MISMATCH", got["4:Unit"])
	assert.False(t, preview.GetCanConfirm())
}

// join adds a staff member whose role holds permissions to the garage.
func join(t *testing.T, owner *fixture.Actor, garageID, label string, permissions ...tenantv1.Permission) *fixture.Actor {
	t.Helper()

	ctx := t.Context()

	role, err := owner.API.CreateRole(ctx, &tenantv1.Role{Name: "QA " + label, Permissions: permissions})
	require.NoError(t, err)

	group, err := owner.API.CreateStaffGroup(ctx, garageID, &tenantv1.StaffGroup{Name: "QA " + label, RoleId: role.GetId()})
	require.NoError(t, err)

	member := fixture.NewVerifiedOwner(t, "inv-import-"+label)

	candidate, err := owner.API.LookupStaffCandidate(ctx, garageID, member.Email)
	require.NoError(t, err)

	_, err = owner.API.CreateInvitation(ctx, garageID, candidate.GetUserId(), []string{group.GetId()})
	require.NoError(t, err)

	invitations, err := member.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, invitations)

	_, err = member.API.AcceptInvitation(ctx, invitations[0].GetId())
	require.NoError(t, err)

	return member
}

// Scenario: E2E-INV-037, Release 7.2: what an import needs follows the file:
// the type, then the columns. The templates download for anyone who may
// import.
func TestImportPermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-import-perms")
	garageID := garage.GetId()
	acme := supplier(t, owner.API, garageID, "ACME ")

	read := tenantv1.Permission_PERMISSION_INVENTORY_READ
	reader := join(t, owner, garageID, "reader", read)
	catalogueOnly := join(t, owner, garageID, "catalogue", tenantv1.Permission_PERMISSION_CATALOGUE_READ, tenantv1.Permission_PERMISSION_CATALOGUE_WRITE)
	writer := join(t, owner, garageID, "writer", read, tenantv1.Permission_PERMISSION_INVENTORY_WRITE)
	staff := join(t, owner, garageID, "staff", read, tenantv1.Permission_PERMISSION_INVENTORY_WRITE, tenantv1.Permission_PERMISSION_INVENTORY_COST_READ,
		tenantv1.Permission_PERMISSION_INVENTORY_COUNT, tenantv1.Permission_PERMISSION_CATALOGUE_READ, tenantv1.Permission_PERMISSION_CATALOGUE_WRITE)

	withStock := partsRows(3, acme.GetName())
	withStock[0]["Opening quantity"], withStock[0]["Unit cost"], withStock[0]["Supplier"] = "2", "500", acme.GetName()
	stockFile := workbook(t, partsColumns, withStock)

	plain := cloneRows(withStock)
	for _, r := range plain {
		delete(r, "Opening quantity")
		delete(r, "Unit cost")
		delete(r, "Supplier")
	}

	plainFile := workbook(t, partsColumns, plain)

	t.Run("a reader may import nothing", func(t *testing.T) {
		for typ, columns := range map[inventoryv1.ImportType][]string{typeParts: partsColumns, typeReceipts: receiptsColumns, typeCounts: countsColumns} {
			file := workbook(t, columns, nil)

			preview, err := reader.API.PreviewImport(ctx, garageID, typ, "x.xlsx", file)
			require.NoError(t, err)
			assert.False(t, preview.GetCanConfirm())
			assert.Equal(t, "PERMISSION_REQUIRED", problems(preview)["0:"], typ.String())

			_, err = reader.API.ConfirmImport(ctx, garageID, typ, "x.xlsx", file)
			fixture.RequireAPIError(t, err, http.StatusForbidden, "")
		}
	})

	t.Run("catalogue write alone cannot bring opening stock or cost", func(t *testing.T) {
		preview, err := catalogueOnly.API.PreviewImport(ctx, garageID, typeParts, "stock.xlsx", stockFile)
		require.NoError(t, err)

		got := problems(preview)
		assert.Equal(t, "PERMISSION_REQUIRED", got["0:Opening quantity"])
		assert.Equal(t, "PERMISSION_REQUIRED", got["0:Unit cost"])
		assert.False(t, preview.GetCanConfirm())

		_, err = catalogueOnly.API.ConfirmImport(ctx, garageID, typeParts, "stock.xlsx", stockFile)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "IMPORT_FILE_INVALID")

		parts, err := owner.API.ListParts(ctx, garageID, nil)
		require.NoError(t, err)
		assert.Empty(t, parts, "nothing was created")

		_, err = catalogueOnly.API.ConfirmImport(ctx, garageID, typeParts, "plain.xlsx", plainFile)
		require.NoError(t, err, "a parts file without stock or cost needs only catalogue write")
	})

	t.Run("inventory write without cost access cannot import unit costs", func(t *testing.T) {
		costed := workbook(t, receiptsColumns, []row{{"SKU": "P-0001", "Quantity": "1", "Supplier": acme.GetName(), "Unit cost": "900"}})

		preview, err := writer.API.PreviewImport(ctx, garageID, typeReceipts, "gr.xlsx", costed)
		require.NoError(t, err)
		assert.Equal(t, "PERMISSION_REQUIRED", problems(preview)["0:Unit cost"])

		free := workbook(t, receiptsColumns, []row{{"SKU": "P-0001", "Quantity": "1", "Supplier": acme.GetName()}})
		_, err = writer.API.ConfirmImport(ctx, garageID, typeReceipts, "gr2.xlsx", free)
		require.NoError(t, err)
	})

	t.Run("inventory staff import all three types", func(t *testing.T) {
		more := taggedPartsRows(5, acme.GetName(), "S")

		_, err := staff.API.ConfirmImport(ctx, garageID, typeParts, "staff.xlsx", workbook(t, partsColumns, more))
		require.NoError(t, err)

		_, err = staff.API.ConfirmImport(ctx, garageID, typeReceipts, "staff-gr.xlsx",
			workbook(t, receiptsColumns, []row{{"SKU": "PS-0001", "Quantity": "3", "Supplier": acme.GetName(), "Unit cost": "800"}}))
		require.NoError(t, err)

		_, err = staff.API.ConfirmImport(ctx, garageID, typeCounts, "staff-count.xlsx",
			workbook(t, countsColumns, []row{{"SKU": "PS-0001", "Counted quantity": "3"}}))
		require.NoError(t, err)
	})

	t.Run("the templates carry the headers and the instructions", func(t *testing.T) {
		for typ, columns := range map[inventoryv1.ImportType][]string{typeParts: partsColumns, typeReceipts: receiptsColumns, typeCounts: countsColumns} {
			contentType, data, err := owner.API.DownloadImportTemplate(ctx, garageID, typ)
			require.NoError(t, err, typ.String())
			assert.Contains(t, contentType, "spreadsheetml", typ.String())

			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err)
			assert.Equal(t, []string{"Data", "Instructions"}, f.GetSheetList())

			rows, err := f.GetRows("Data")
			require.NoError(t, err)
			require.Len(t, rows, 1, "only the header row, nothing that could be imported by mistake")
			assert.Equal(t, columns, rows[0])

			help, err := f.GetRows("Instructions")
			require.NoError(t, err)
			assert.Len(t, help, len(columns)+1)
			require.NoError(t, f.Close())
		}
	})
}

// Scenario: E2E-INV-038, Release 7.2: a file that cannot be read, is too
// large, has too many rows or lacks a column is refused as a whole; headers
// are matched without regard to case and spacing, extra columns are ignored
// and blank rows are skipped.
func TestImportRefusesBadFiles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-import-bad")
	garageID := garage.GetId()

	firstError := func(t *testing.T, content []byte) *inventoryv1.ImportRowError {
		t.Helper()

		preview, err := owner.API.PreviewImport(ctx, garageID, typeParts, "x.xlsx", content)
		require.NoError(t, err)
		assert.False(t, preview.GetCanConfirm())
		require.NotEmpty(t, preview.GetErrors())

		return preview.GetErrors()[0]
	}

	t.Run("not a workbook", func(t *testing.T) {
		e := firstError(t, []byte("SKU,Name\nA-1,Pad\n"))
		assert.Equal(t, "INVALID_FILE", e.GetCode())
		assert.Zero(t, e.GetRow())
	})

	t.Run("a required column is missing", func(t *testing.T) {
		e := firstError(t, workbook(t, []string{"Name", "Base unit", "Base unit abbreviation"}, []row{{"Name": "Pad", "Base unit": "Piece", "Base unit abbreviation": "pc"}}))
		assert.Equal(t, "MISSING_COLUMN", e.GetCode())
		assert.Equal(t, "SKU", e.GetColumn())
	})

	t.Run("too many rows", func(t *testing.T) {
		rows := make([]row, 5001)
		for i := range rows {
			rows[i] = row{"SKU": fmt.Sprintf("X-%05d", i), "Name": "x", "Base unit": "Piece", "Base unit abbreviation": "pc"}
		}

		e := firstError(t, workbook(t, partsColumns, rows))
		assert.Equal(t, "TOO_MANY_ROWS", e.GetCode())
	})

	t.Run("too large", func(t *testing.T) {
		e := firstError(t, bytes.Repeat([]byte("x"), 5<<20+1))
		assert.Equal(t, "FILE_TOO_LARGE", e.GetCode())
	})

	t.Run("headers in other case, an extra column and blank rows are fine", func(t *testing.T) {
		headers := []string{"sku", " NAME ", "base  unit", "Base Unit Abbreviation", "Whatever"}
		file := workbook(t, headers, []row{
			{"sku": "A-1", " NAME ": "Pad", "base  unit": "Piece", "Base Unit Abbreviation": "pc", "Whatever": "ignored"},
			{},
			{"sku": "A-2", " NAME ": "Disc", "base  unit": "Piece", "Base Unit Abbreviation": "pc"},
		})

		preview, err := owner.API.PreviewImport(ctx, garageID, typeParts, "ok.xlsx", file)
		require.NoError(t, err)
		require.True(t, preview.GetCanConfirm(), "%v", preview.GetErrors())
		assert.EqualValues(t, 2, preview.GetTotalRows(), "the blank row is not counted")

		rows := preview.GetSampleRows()
		require.Len(t, rows, 2)
		assert.EqualValues(t, 2, rows[0].GetRow())
		assert.EqualValues(t, 4, rows[1].GetRow(), "numbered as the sheet shows them, blank row 3 included")
	})
}
