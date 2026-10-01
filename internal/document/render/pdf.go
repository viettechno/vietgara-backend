// Package render turns plain data structs into PDF/Excel bytes (FRD
// 3.13). It has no database or use-case dependencies and knows nothing
// about any other module -- the document use case reads the data through
// its own narrow reader interfaces and hands this package only what a
// layout needs, the same cross-module decoupling pattern used everywhere
// else (HLD Section 2). No templates are stored (FR-DOC-03, customizing
// the logo and layout, is Phase 4): the layout below is the garage's one
// standard layout for every document of its kind.
package render

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/go-pdf/fpdf"
)

// GarageHeader is the letterhead shown at the top of every PDF.
type GarageHeader struct {
	Name    string
	Address string
	Phone   string
}

// DocumentLine is one line item row (a quote line, a repair order line).
type DocumentLine struct {
	Description string
	Quantity    string
	UnitPrice   int64
	LineTotal   int64
}

func newPDF() *fpdf.Fpdf {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(18, 18, 18)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", 10)

	return pdf
}

func writeHeader(pdf *fpdf.Fpdf, garage GarageHeader, title, number string) {
	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(0, 8, garage.Name, "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)

	if garage.Address != "" {
		pdf.CellFormat(0, 5, garage.Address, "", 1, "L", false, 0, "")
	}

	if garage.Phone != "" {
		pdf.CellFormat(0, 5, garage.Phone, "", 1, "L", false, 0, "")
	}

	pdf.Ln(4)
	pdf.SetFont("Helvetica", "B", 14)
	pdf.CellFormat(0, 8, title, "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 6, "No. "+number, "", 1, "L", false, 0, "")
	pdf.Ln(4)
}

func writeKeyValue(pdf *fpdf.Fpdf, label, value string) {
	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(40, 6, label, "", 0, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 6, value, "", 1, "L", false, 0, "")
}

var lineColumnWidths = [4]float64{90, 25, 35, 35}

func writeLineTable(pdf *fpdf.Fpdf, lines []DocumentLine, total int64) {
	pdf.Ln(2)
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetFillColor(240, 240, 240)

	headers := [4]string{"Description", "Qty", "Unit Price", "Line Total"}

	for i, h := range headers {
		pdf.CellFormat(lineColumnWidths[i], 7, h, "1", 0, "L", true, 0, "")
	}

	pdf.Ln(-1)

	pdf.SetFont("Helvetica", "", 10)

	for _, line := range lines {
		pdf.CellFormat(lineColumnWidths[0], 7, line.Description, "1", 0, "L", false, 0, "")
		pdf.CellFormat(lineColumnWidths[1], 7, line.Quantity, "1", 0, "R", false, 0, "")
		pdf.CellFormat(lineColumnWidths[2], 7, formatVND(line.UnitPrice), "1", 0, "R", false, 0, "")
		pdf.CellFormat(lineColumnWidths[3], 7, formatVND(line.LineTotal), "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}

	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(lineColumnWidths[0]+lineColumnWidths[1]+lineColumnWidths[2], 8, "Total", "1", 0, "R", false, 0, "")
	pdf.CellFormat(lineColumnWidths[3], 8, formatVND(total), "1", 0, "R", false, 0, "")
	pdf.Ln(-1)
}

func finish(pdf *fpdf.Fpdf) ([]byte, error) {
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("render pdf: %w", err)
	}

	return buf.Bytes(), nil
}

// formatVND renders an integer VND amount with thousands separators,
// e.g. 1234567 -> "1,234,567 VND".
func formatVND(amount int64) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}

	digits := strconv.FormatInt(amount, 10)
	grouped := make([]byte, 0, len(digits)+len(digits)/3)

	for i, d := range []byte(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped = append(grouped, ',')
		}

		grouped = append(grouped, d)
	}

	sign := ""
	if negative {
		sign = "-"
	}

	return sign + string(grouped) + " VND"
}
