package render

import (
	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// purchaseOrderTemplate renders a purchase order to a supplier (FR-INV-16).
// Prices are printed only when the model says so.
func purchaseOrderTemplate(data printmodel.PurchaseOrder, o Options) ([]byte, error) {
	lay := layoutFor(o.Paper)
	pdf := newPDF(lay, copyLabel(o, "COPY"))
	writeHeader(pdf, lay, data.Garage, "PURCHASE ORDER", data.Number)

	writeKeyValue(pdf, "Supplier", data.SupplierName)
	writeKeyValue(pdf, "Date", data.Date)

	if data.ExpectedDate != "" {
		writeKeyValue(pdf, "Expected", data.ExpectedDate)
	}

	if data.Notes != "" {
		writeKeyValue(pdf, "Notes", data.Notes)
	}

	widths := lay.order
	headers := [5]string{headingSKU, headingPart, "Qty", "Unit cost", "Amount"}
	count := len(widths)

	if !data.ShowCost {
		count = 3
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
		cells := [5]string{l.SKU, l.Name, join(l.Quantity, l.Unit), formatVND(l.UnitCost), formatVND(l.Amount)}

		for i := range count {
			align := "L"
			if i >= 2 {
				align = "R"
			}

			pdf.CellFormat(widths[i], 7, cells[i], "1", 0, align, false, 0, "")
		}

		pdf.Ln(-1)
	}

	if data.ShowCost {
		pdf.SetFont(fontFamily, "B", 10)
		pdf.CellFormat(widths[0]+widths[1]+widths[2]+widths[3], 8, labelTotal, "1", 0, "R", false, 0, "")
		pdf.CellFormat(widths[4], 8, formatVND(data.TotalAmount), "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}

	return finish(pdf)
}
