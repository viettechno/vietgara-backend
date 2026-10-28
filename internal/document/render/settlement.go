package render

import "github.com/viettechno/vietgara-backend/internal/document/printmodel"

// settlementTemplate renders a settlement as a receipt (FR-DOC-01).
func settlementTemplate(data printmodel.Settlement, o Options) ([]byte, error) {
	lay := layoutFor(o.Paper)
	pdf := newPDF(lay, copyLabel(o, "COPY"))
	writeHeader(pdf, lay, data.Garage, "RECEIPT", data.Number)

	writeBillTo(pdf, data.BillTo)
	writeKeyValue(pdf, "Customer", data.CustomerName)
	writeKeyValue(pdf, "Vehicle", data.VehiclePlate)
	writeKeyValue(pdf, "Status", data.Status)
	writeKeyValue(pdf, "Paid", formatVND(data.PaidAmount))

	pdf.Ln(4)
	pdf.SetFont(fontFamily, "B", 12)
	pdf.CellFormat(0, 7, "Total: "+formatVND(data.TotalAmount), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	pdf.SetFont(fontFamily, "B", 10)
	pdf.CellFormat(0, 6, "Payments received", "", 1, "L", false, 0, "")

	if len(data.Payments) == 0 {
		pdf.SetFont(fontFamily, "", 10)
		pdf.CellFormat(0, 6, "None yet.", "", 1, "L", false, 0, "")

		return finish(pdf)
	}

	widths := lay.payments
	headers := [3]string{"Date", "Method", "Amount"}

	pdf.SetFillColor(240, 240, 240)

	for i, h := range headers {
		pdf.CellFormat(widths[i], 7, h, "1", 0, "L", true, 0, "")
	}

	pdf.Ln(-1)

	pdf.SetFont(fontFamily, "", 10)

	for _, p := range data.Payments {
		pdf.CellFormat(widths[0], 7, p.Date, "1", 0, "L", false, 0, "")
		pdf.CellFormat(widths[1], 7, p.Method, "1", 0, "L", false, 0, "")
		pdf.CellFormat(widths[2], 7, formatVND(p.Amount), "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}

	return finish(pdf)
}
