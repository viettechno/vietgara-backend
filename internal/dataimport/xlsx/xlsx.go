// Package xlsx reads the first sheet of an uploaded .xlsx workbook. It opens
// the file defensively: the payload must be a ZIP workbook, the uncompressed
// size is bounded, and every cell is read as text so a SKU or barcode keeps
// its leading zeros.
package xlsx

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
)

// Uncompressed bounds for a 5 MB workbook; they stop a zip bomb without
// limiting a normal template.
const (
	unzipSizeLimit    = 64 << 20
	unzipXMLSizeLimit = 16 << 20
	// maxReadRows bounds memory even when a malicious sheet has millions of
	// rows; the use case reports TOO_MANY_ROWS as soon as it sees more than
	// the allowed data rows.
	maxReadRows = domain.MaxRows + 5
)

// Row is one non-empty sheet row; Number is the 1-based row as the sheet
// shows it.
type Row struct {
	Number int
	Cells  []string
}

// Sheet is the first sheet of a workbook.
type Sheet struct {
	Name string
	Rows []Row
}

// Read parses the first sheet. It returns domain.ErrFileInvalid for anything
// that is not a readable Excel workbook.
func Read(content []byte) (*Sheet, error) {
	if len(content) < 4 || !bytes.HasPrefix(content, []byte("PK\x03\x04")) {
		return nil, domain.ErrFileInvalid
	}

	f, err := excelize.OpenReader(bytes.NewReader(content), excelize.Options{
		UnzipSizeLimit:    unzipSizeLimit,
		UnzipXMLSizeLimit: unzipXMLSizeLimit,
		RawCellValue:      true,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrFileInvalid, err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, domain.ErrFileInvalid
	}

	rows, err := f.Rows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrFileInvalid, err)
	}
	defer func() { _ = rows.Close() }()

	sheet := &Sheet{Name: sheets[0]}
	number := 0

	for rows.Next() {
		number++

		if len(sheet.Rows) >= maxReadRows {
			break
		}

		cells, err := rows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, fmt.Errorf("%w: %w", domain.ErrFileInvalid, err)
		}

		if cells = trimTrailingEmpty(cells); len(cells) == 0 {
			continue
		}

		sheet.Rows = append(sheet.Rows, Row{Number: number, Cells: cells})
	}

	if err := rows.Error(); err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrFileInvalid, err)
	}

	return sheet, nil
}

// trimTrailingEmpty drops trailing empty cells, so "a,b,," reads as two
// cells and a blank row reads as none.
func trimTrailingEmpty(cells []string) []string {
	end := len(cells)
	for end > 0 && cells[end-1] == "" {
		end--
	}

	return cells[:end]
}
