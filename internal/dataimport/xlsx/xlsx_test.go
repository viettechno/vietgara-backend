package xlsx_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/xlsx"
)

// workbook renders rows into a one-sheet .xlsx; a nil row leaves a blank line.
func workbook(t *testing.T, rows [][]any) []byte {
	t.Helper()

	f := excelize.NewFile()

	defer func() { _ = f.Close() }()

	for i, row := range rows {
		if row == nil {
			continue
		}

		cell, err := excelize.CoordinatesToCellName(1, i+1)
		require.NoError(t, err)
		require.NoError(t, f.SetSheetRow("Sheet1", cell, &row))
	}

	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))

	return buf.Bytes()
}

func TestReadKeepsSheetRowNumbersAndSkipsBlankRows(t *testing.T) {
	t.Parallel()

	sheet, err := xlsx.Read(workbook(t, [][]any{
		{"SKU", "Name"},
		{"A-1", "Brake pad", "", ""},
		nil,
		{"A-2", "Oil", ""},
		{"", ""},
	}))
	require.NoError(t, err)

	require.Len(t, sheet.Rows, 3, "blank rows are dropped")
	assert.Equal(t, 1, sheet.Rows[0].Number)
	assert.Equal(t, 2, sheet.Rows[1].Number)
	assert.Equal(t, []string{"A-1", "Brake pad"}, sheet.Rows[1].Cells, "trailing empty cells are trimmed")
	assert.Equal(t, 4, sheet.Rows[2].Number, "the numbering still counts the blank row 3")
}

func TestReadReturnsTextAsTyped(t *testing.T) {
	t.Parallel()

	sheet, err := xlsx.Read(workbook(t, [][]any{{"SKU", "Qty", "Price"}, {"007", 12, 2500.5}}))
	require.NoError(t, err)

	assert.Equal(t, []string{"007", "12", "2500.5"}, sheet.Rows[1].Cells, "a text code keeps its leading zeros")
}

func TestReadRejectsWhatIsNotAWorkbook(t *testing.T) {
	t.Parallel()

	for name, content := range map[string][]byte{
		"empty":        nil,
		"plain text":   []byte("SKU,Name\nA-1,Pad"),
		"too short":    []byte("PK"),
		"broken zip":   append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0x7f}, 64)...),
		"legacy xls":   {0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1},
		"pdf document": []byte("%PDF-1.7 ..."),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := xlsx.Read(content)
			require.ErrorIs(t, err, domain.ErrFileInvalid)
		})
	}
}

func TestReadStopsAfterMoreRowsThanAnImportAllows(t *testing.T) {
	t.Parallel()

	rows := make([][]any, 0, domain.MaxRows+100)
	for i := range domain.MaxRows + 100 {
		rows = append(rows, []any{fmt.Sprintf("SKU-%d", i)})
	}

	sheet, err := xlsx.Read(workbook(t, rows))
	require.NoError(t, err)

	assert.Len(t, sheet.Rows, domain.MaxRows+5, "enough rows for the use case to report TOO_MANY_ROWS, not the whole sheet")
}
