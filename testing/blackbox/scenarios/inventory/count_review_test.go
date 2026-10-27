package inventory_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-INV-021, Release 7.1, counting in units (FR-INV-12,
// FR-CAT-02): a count typed in a packaging unit is converted to the base
// unit; zero is a real count and an empty value makes the line uncounted
// again; each typing takes the system quantity afresh, so stock received
// after a line was counted shows only once the line is typed again.
func TestStockCountInPackagingUnitsAndZero(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-count-units")
	garageID := garage.GetId()
	parts := stockedParts(t, owner, garageID, litre(t, owner, garageID), stockLot{"10", 100}, stockLot{"4", 50})
	oil, filter := parts[0], parts[1]

	can := fixture.NewUnit(t, owner.API, garageID, "Can", "can")
	_, err := owner.API.SetPartUnits(ctx, garageID, oil.GetId(), []*cataloguev1.PackagingUnit{{UnitId: can.GetId(), Factor: "5"}})
	require.NoError(t, err)

	count, err := owner.API.CreateStockCount(ctx, garageID, &inventoryv1.StockCount{Name: "Shelf B"})
	require.NoError(t, err)

	count, err = owner.API.AddStockCountLines(ctx, garageID, count.GetId(), []string{oil.GetId(), filter.GetId()}, "")
	require.NoError(t, err)

	// 2 cans of 5 L are the 10 L on hand.
	count, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), lineOfPart(t, count, oil).GetId(), can.GetId(), "2")
	require.NoError(t, err)

	line := lineOfPart(t, count, oil)
	assert.Equal(t, can.GetId(), line.GetUnitId())
	assert.Equal(t, "2", line.GetEnteredCountedQuantity())
	assert.Equal(t, "10", line.GetCountedQuantity(), "converted to the base unit")
	assert.Equal(t, inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_MATCHED, line.GetResult())

	// Zero is a count, not an empty line.
	count, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), lineOfPart(t, count, filter).GetId(), "", "0")
	require.NoError(t, err)

	line = lineOfPart(t, count, filter)
	assert.Equal(t, "0", line.GetCountedQuantity())
	assert.Equal(t, inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_SHORT, line.GetResult())
	assert.Equal(t, "-4", line.GetDifferenceQuantity())
	assert.EqualValues(t, 0, count.GetUncountedLines())

	// Clearing the value makes the line uncounted again.
	count, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), lineOfPart(t, count, filter).GetId(), "", "")
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_UNCOUNTED, lineOfPart(t, count, filter).GetResult())
	assert.EqualValues(t, 1, count.GetUncountedLines())

	// Stock received after the line was counted leaves its snapshot alone...
	_, err = owner.API.ReceiveStock(ctx, garageID, oil.GetId(), "2", "", 100)
	require.NoError(t, err)

	count, err = owner.API.GetStockCount(ctx, garageID, count.GetId())
	require.NoError(t, err)
	assert.Equal(t, "10", lineOfPart(t, count, oil).GetSystemQuantity(), "the quantity it was counted against")

	// ...until the line is typed again, which compares with the stock now.
	count, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), lineOfPart(t, count, oil).GetId(), can.GetId(), "2")
	require.NoError(t, err)

	line = lineOfPart(t, count, oil)
	assert.Equal(t, "12", line.GetSystemQuantity())
	assert.Equal(t, "-2", line.GetDifferenceQuantity())
	assert.Equal(t, inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_SHORT, line.GetResult())
}

// Scenario: E2E-INV-024, Release 7.1, count workbook (FR-INV-08, FR-DOC-02,
// FR-INV-12): the Excel export of a count lists the system and counted
// quantities, the result and the difference; the average cost and the
// difference value are there only for a caller with inventory.cost.read. A
// technician may export the count, since it is a read of inventory, and never
// sees a cost.
func TestStockCountExcelHidesCostWithoutAccess(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	// English, so that the default staff groups have the English names.
	owner := fixture.NewVerifiedOwnerWithLocale(t, "inv-count-xlsx", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox count workbook"})
	require.NoError(t, err)

	garageID := garage.GetId()
	parts := stockedParts(t, owner, garageID, litre(t, owner, garageID), stockLot{"10", 100}, stockLot{"20", 50})
	oil, filter := parts[0], parts[1]
	tech := joinAs(t, owner, garageID, "Technician", "inv-count-xlsx-tech")

	count, err := owner.API.CreateStockCount(ctx, garageID, &inventoryv1.StockCount{Name: "Shelf A"})
	require.NoError(t, err)

	count, err = owner.API.AddStockCountLines(ctx, garageID, count.GetId(), []string{oil.GetId(), filter.GetId()}, "")
	require.NoError(t, err)

	for part, counted := range map[*cataloguev1.Part]string{oil: "10", filter: "18"} {
		count, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), lineOfPart(t, count, part).GetId(), "", counted)
		require.NoError(t, err)
	}

	withCost := countSheet(t, owner.API, garageID, count.GetId())
	assert.Equal(t, []string{
		"SKU", "Part", "Unit", "System", "Counted", "Difference", "Result", "Average cost (VND)", "Difference value (VND)",
	}, withCost[0])

	row := sheetRow(t, withCost, filter.GetSku())
	assert.Equal(t, []string{"20", "18", "-2"}, row[3:6])
	assert.Equal(t, []string{"50", "-100"}, row[7:9], "valued at average cost")

	quantitiesOnly := countSheet(t, tech.API, garageID, count.GetId())
	assert.Equal(t, []string{"SKU", "Part", "Unit", "System", "Counted", "Difference", "Result"}, quantitiesOnly[0])

	for _, r := range quantitiesOnly {
		assert.LessOrEqual(t, len(r), 7, "no cost column: %v", r)
	}

	row = sheetRow(t, quantitiesOnly, filter.GetSku())
	assert.Equal(t, []string{"20", "18", "-2"}, row[3:6])
}

// countSheet downloads a count's workbook as api and returns its rows.
func countSheet(t *testing.T, api *client.Client, garageID, countID string) [][]string {
	t.Helper()

	contentType, body, err := api.DownloadStockCountExcel(t.Context(), garageID, countID)
	require.NoError(t, err)
	assert.Contains(t, contentType, "spreadsheetml")

	book, err := excelize.OpenReader(bytes.NewReader(body))
	require.NoError(t, err)

	defer func() { _ = book.Close() }()

	rows, err := book.GetRows("Stock Count")
	require.NoError(t, err)
	require.NotEmpty(t, rows)

	return rows
}

// sheetRow returns the row whose first cell is sku.
func sheetRow(t *testing.T, rows [][]string, sku string) []string {
	t.Helper()

	for _, r := range rows {
		if len(r) > 0 && r[0] == sku {
			return r
		}
	}

	t.Fatalf("no row for %s in %v", sku, rows)

	return nil
}
