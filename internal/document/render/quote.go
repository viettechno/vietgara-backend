package render

import "github.com/viettechno/vietgara-backend/internal/document/printmodel"

// quoteTemplate renders a quote (FR-DOC-01).
func quoteTemplate(data printmodel.Quote, o Options) ([]byte, error) {
	lay := layoutFor(o.Paper)
	pdf := newPDF(lay, copyLabel(o, "COPY"))
	writeHeader(pdf, lay, data.Garage, "QUOTE", data.Number)

	writeBillTo(pdf, data.BillTo)
	writeKeyValue(pdf, "Customer", data.CustomerName)
	writeKeyValue(pdf, "Vehicle", data.VehiclePlate)
	writeKeyValue(pdf, "Status", data.Status)

	writeLineTable(pdf, lay, data.Lines, data.TotalAmount)

	if data.Notes != "" {
		pdf.Ln(4)
		pdf.SetFont(fontFamily, "B", 10)
		pdf.CellFormat(0, 6, "Notes", "", 1, "L", false, 0, "")
		pdf.SetFont(fontFamily, "", 10)
		pdf.MultiCell(0, 5, data.Notes, "", "L", false)
	}

	return finish(pdf)
}
