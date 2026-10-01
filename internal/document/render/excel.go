package render

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// StockMovementRow is one row of the stock movement report (FR-INV-05).
type StockMovementRow struct {
	Date      string
	SKU       string
	ItemName  string
	Direction string
	Quantity  string
	Reference string
}

const stockMovementsSheet = "Stock Movements"

// StockMovementsExcel renders the stock in/out/on-hand movement
// report as an Excel workbook (FR-DOC-02).
func StockMovementsExcel(rows []StockMovementRow) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", stockMovementsSheet); err != nil {
		return nil, fmt.Errorf("render stock movements excel: %w", err)
	}

	headerStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, fmt.Errorf("render stock movements excel: %w", err)
	}

	headers := []string{"Date", "SKU", "Item", "Direction", "Quantity", "Reference"}
	if err := f.SetSheetRow(stockMovementsSheet, "A1", &headers); err != nil {
		return nil, fmt.Errorf("render stock movements excel: %w", err)
	}

	if err := f.SetRowStyle(stockMovementsSheet, 1, 1, headerStyle); err != nil {
		return nil, fmt.Errorf("render stock movements excel: %w", err)
	}

	for i, row := range rows {
		cells := []string{row.Date, row.SKU, row.ItemName, row.Direction, row.Quantity, row.Reference}
		if err := f.SetSheetRow(stockMovementsSheet, fmt.Sprintf("A%d", i+2), &cells); err != nil {
			return nil, fmt.Errorf("render stock movements excel: %w", err)
		}
	}

	for col, width := range map[string]float64{"A": 18, "B": 16, "C": 28, "D": 12, "E": 12, "F": 24} {
		if err := f.SetColWidth(stockMovementsSheet, col, col, width); err != nil {
			return nil, fmt.Errorf("render stock movements excel: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("render stock movements excel: %w", err)
	}

	return buf.Bytes(), nil
}
