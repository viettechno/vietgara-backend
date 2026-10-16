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
	// Quantity and BaseUnit are the base-unit amount; EnteredQuantity and
	// EnteredUnit what the user typed when it was another unit (FR-CAT-02).
	Quantity        string
	BaseUnit        string
	EnteredQuantity string
	EnteredUnit     string
	Reference       string
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

	headers := []string{"Date", "SKU", "Part", "Direction", "Quantity", "Unit", "Entered quantity", "Entered unit", "Reference"}
	if err := f.SetSheetRow(stockMovementsSheet, "A1", &headers); err != nil {
		return nil, fmt.Errorf("render stock movements excel: %w", err)
	}

	if err := f.SetRowStyle(stockMovementsSheet, 1, 1, headerStyle); err != nil {
		return nil, fmt.Errorf("render stock movements excel: %w", err)
	}

	for i, row := range rows {
		cells := []string{
			row.Date, row.SKU, row.ItemName, row.Direction, row.Quantity, row.BaseUnit, row.EnteredQuantity, row.EnteredUnit,
			row.Reference,
		}
		if err := f.SetSheetRow(stockMovementsSheet, fmt.Sprintf("A%d", i+2), &cells); err != nil {
			return nil, fmt.Errorf("render stock movements excel: %w", err)
		}
	}

	for col, width := range map[string]float64{"A": 18, "B": 16, "C": 28, "D": 12, "E": 12, "F": 10, "G": 16, "H": 14, "I": 24} {
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

// RevenueRow is one bucket of the revenue report (FR-RPT-01).
type RevenueRow struct {
	Period string
	Amount int64
}

const revenueSheet = "Revenue"

// RevenueExcel renders the revenue-by-period report as an Excel workbook
// (FR-DOC-02): one row per bucket and a total row. Amounts are numbers, so
// the sheet can be summed and charted.
func RevenueExcel(rows []RevenueRow, total int64) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", revenueSheet); err != nil {
		return nil, fmt.Errorf("render revenue excel: %w", err)
	}

	headerStyle, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, fmt.Errorf("render revenue excel: %w", err)
	}

	headers := []any{"Period", "Revenue (VND)"}
	if err := f.SetSheetRow(revenueSheet, "A1", &headers); err != nil {
		return nil, fmt.Errorf("render revenue excel: %w", err)
	}

	if err := f.SetRowStyle(revenueSheet, 1, 1, headerStyle); err != nil {
		return nil, fmt.Errorf("render revenue excel: %w", err)
	}

	for i, row := range rows {
		cells := []any{row.Period, row.Amount}
		if err := f.SetSheetRow(revenueSheet, fmt.Sprintf("A%d", i+2), &cells); err != nil {
			return nil, fmt.Errorf("render revenue excel: %w", err)
		}
	}

	totalRow := len(rows) + 2
	cells := []any{"Total", total}

	if err := f.SetSheetRow(revenueSheet, fmt.Sprintf("A%d", totalRow), &cells); err != nil {
		return nil, fmt.Errorf("render revenue excel: %w", err)
	}

	if err := f.SetRowStyle(revenueSheet, totalRow, totalRow, headerStyle); err != nil {
		return nil, fmt.Errorf("render revenue excel: %w", err)
	}

	for col, width := range map[string]float64{"A": 18, "B": 18} {
		if err := f.SetColWidth(revenueSheet, col, col, width); err != nil {
			return nil, fmt.Errorf("render revenue excel: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("render revenue excel: %w", err)
	}

	return buf.Bytes(), nil
}
