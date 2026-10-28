package render

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// Column headings the inventory documents share with the stock movement
// report.
const (
	headingSKU      = "SKU"
	headingPart     = "Part"
	headingQuantity = "Quantity"
	headingUnit     = "Unit"
	labelTotal      = "Total"
)

// goodsReceiptTemplate renders a goods receipt (FR-INV-06).
func goodsReceiptTemplate(data printmodel.GoodsReceipt, o Options) ([]byte, error) {
	lay := layoutFor(o.Paper)
	pdf := newPDF(lay, copyLabel(o, "COPY"))
	writeHeader(pdf, lay, data.Garage, "GOODS RECEIPT", data.Number)

	writeKeyValue(pdf, "Date", data.Date)
	writeKeyValue(pdf, "Status", data.Status)

	if data.DeliveryNoteNumber != "" {
		writeKeyValue(pdf, "Delivery note", data.DeliveryNoteNumber)
	}

	if data.Notes != "" {
		writeKeyValue(pdf, "Notes", data.Notes)
	}

	widths := lay.goods
	headers := [6]string{headingSKU, headingPart, "Qty", "Supplier", "Unit cost", "Amount"}
	count := len(widths)

	if !data.ShowCost {
		count = 4
	}

	pdf.Ln(2)
	pdf.SetFont(fontFamily, "B", 9)
	pdf.SetFillColor(240, 240, 240)

	for i := range count {
		pdf.CellFormat(widths[i], 7, headers[i], "1", 0, "L", true, 0, "")
	}

	pdf.Ln(-1)
	pdf.SetFont(fontFamily, "", 9)

	for _, l := range data.Lines {
		cells := [6]string{l.SKU, l.Name, join(l.Quantity, l.Unit), l.Supplier, formatVND(l.UnitCost), formatVND(l.Amount)}

		for i := range count {
			align := "L"
			if i >= 2 && i != 3 {
				align = "R"
			}

			pdf.CellFormat(widths[i], 7, cells[i], "1", 0, align, false, 0, "")
		}

		pdf.Ln(-1)
	}

	if data.ShowCost {
		pdf.SetFont(fontFamily, "B", 10)
		pdf.CellFormat(widths[0]+widths[1]+widths[2]+widths[3]+widths[4], 8, labelTotal, "1", 0, "R", false, 0, "")
		pdf.CellFormat(widths[5], 8, formatVND(data.TotalAmount), "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}

	return finish(pdf)
}

// stockIssueTemplate renders a stock issue (phiếu xuất kho). It prints
// quantities only: no cost is on the document.
func stockIssueTemplate(data printmodel.StockIssue, o Options) ([]byte, error) {
	lay := layoutFor(o.Paper)
	pdf := newPDF(lay, copyLabel(o, "COPY"))
	writeHeader(pdf, lay, data.Garage, "STOCK ISSUE", data.Number)

	writeKeyValue(pdf, "Repair order", data.RepairOrderNumber)
	writeKeyValue(pdf, "Status", data.Status)

	if data.PostedAt != "" {
		writeKeyValue(pdf, "Posted", data.PostedAt)
	}

	if data.PostedBy != "" {
		writeKeyValue(pdf, "Posted by", data.PostedBy)
	}

	widths := lay.stock
	headers := [3]string{headingSKU, headingPart, headingQuantity}

	pdf.Ln(2)
	pdf.SetFont(fontFamily, "B", 10)
	pdf.SetFillColor(240, 240, 240)

	for i, h := range headers {
		pdf.CellFormat(widths[i], 7, h, "1", 0, "L", true, 0, "")
	}

	pdf.Ln(-1)
	pdf.SetFont(fontFamily, "", 10)

	for _, l := range data.Lines {
		pdf.CellFormat(widths[0], 7, l.SKU, "1", 0, "L", false, 0, "")
		pdf.CellFormat(widths[1], 7, l.Name, "1", 0, "L", false, 0, "")
		pdf.CellFormat(widths[2], 7, join(l.Quantity, l.Unit), "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}

	return finish(pdf)
}

func join(quantity, unit string) string {
	if unit == "" {
		return quantity
	}

	return quantity + " " + unit
}

// PurchaseRow is one line of the purchases report.
type PurchaseRow struct {
	Date     string
	Receipt  string
	Supplier string
	SKU      string
	Part     string
	Quantity string
	Unit     string
	UnitCost int64
	Amount   int64
}

const purchasesSheet = "Purchases"

// PurchasesExcel renders the purchases-by-supplier report as an Excel
// workbook (FR-INV-19): amounts are numbers so the sheet can be summed.
func PurchasesExcel(rows []PurchaseRow, total int64) ([]byte, error) {
	data := make([][]any, 0, len(rows))
	for _, r := range rows {
		data = append(data, []any{r.Date, r.Receipt, r.Supplier, r.SKU, r.Part, r.Quantity, r.Unit, r.UnitCost, r.Amount})
	}

	return sheet(purchasesSheet,
		[]any{"Date", "Receipt", "Supplier", headingSKU, headingPart, headingQuantity, headingUnit, "Unit cost (VND)", "Amount (VND)"},
		data, []any{labelTotal, "", "", "", "", "", "", "", total},
		map[string]float64{"A": 12, "B": 16, "C": 24, "D": 14, "E": 28, "F": 12, "G": 8, "H": 16, "I": 16})
}

// StockCountRow is one line of a stock count.
type StockCountRow struct {
	SKU    string
	Part   string
	Unit   string
	System string
	// Counted and Difference are empty while the line is uncounted.
	Counted    string
	Difference string
	Result     string
	// AverageCost and Value are used only when the sheet shows costs.
	AverageCost int64
	Value       int64
}

const stockCountSheet = "Stock Count"

// StockCountExcel renders a stock count as an Excel workbook for review
// (FR-INV-12): system against counted quantity per line and, with showCost,
// the value of the difference, whose total closes the sheet.
func StockCountExcel(rows []StockCountRow, showCost bool, total int64) ([]byte, error) {
	headers := []any{headingSKU, headingPart, headingUnit, "System", "Counted", "Difference", "Result"}
	widths := map[string]float64{"A": 14, "B": 28, "C": 8, "D": 12, "E": 12, "F": 12, "G": 12}
	totals := []any{labelTotal, "", "", "", "", "", ""}

	if showCost {
		headers = append(headers, "Average cost (VND)", "Difference value (VND)")
		widths["H"], widths["I"] = 18, 22

		totals = append(totals, "", total)
	}

	data := make([][]any, 0, len(rows))

	for _, r := range rows {
		row := []any{r.SKU, r.Part, r.Unit, r.System, r.Counted, r.Difference, r.Result}
		if showCost {
			row = append(row, r.AverageCost, r.Value)
		}

		data = append(data, row)
	}

	return sheet(stockCountSheet, headers, data, totals, widths)
}

// ValuationRow is one part of the stock valuation report.
type ValuationRow struct {
	Category    string
	SKU         string
	Part        string
	Quantity    string
	Unit        string
	AverageCost int64
	Value       int64
}

const valuationSheet = "Stock Valuation"

// StockValuationExcel renders the stock valuation report at a date as an
// Excel workbook (FR-INV-07).
func StockValuationExcel(rows []ValuationRow, total int64) ([]byte, error) {
	data := make([][]any, 0, len(rows))
	for _, r := range rows {
		data = append(data, []any{r.Category, r.SKU, r.Part, r.Quantity, r.Unit, r.AverageCost, r.Value})
	}

	return sheet(valuationSheet,
		[]any{"Category", headingSKU, headingPart, headingQuantity, headingUnit, "Average cost (VND)", "Value (VND)"},
		data, []any{labelTotal, "", "", "", "", "", total},
		map[string]float64{"A": 20, "B": 14, "C": 28, "D": 12, "E": 8, "F": 18, "G": 16})
}

// sheet writes a workbook with one sheet: a bold header row, the rows, and a
// bold total row.
func sheet(name string, headers []any, rows [][]any, total []any, widths map[string]float64) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName("Sheet1", name); err != nil {
		return nil, fmt.Errorf("render %s excel: %w", name, err)
	}

	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, fmt.Errorf("render %s excel: %w", name, err)
	}

	all := append(append([][]any{headers}, rows...), total)

	for i, row := range all {
		if err := f.SetSheetRow(name, fmt.Sprintf("A%d", i+1), &row); err != nil {
			return nil, fmt.Errorf("render %s excel: %w", name, err)
		}
	}

	for _, r := range []int{1, len(all)} {
		if err := f.SetRowStyle(name, r, r, bold); err != nil {
			return nil, fmt.Errorf("render %s excel: %w", name, err)
		}
	}

	for col, width := range widths {
		if err := f.SetColWidth(name, col, col, width); err != nil {
			return nil, fmt.Errorf("render %s excel: %w", name, err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("render %s excel: %w", name, err)
	}

	return buf.Bytes(), nil
}
