package render

// QuotePDFData is everything a quote's PDF needs (FR-DOC-01).
type QuotePDFData struct {
	Garage       GarageHeader
	Number       string
	Status       string
	CustomerName string
	VehiclePlate string
	Notes        string
	Lines        []DocumentLine
	TotalAmount  int64
}

// QuotePDF renders a quote as a PDF using the garage's standard layout.
func QuotePDF(data QuotePDFData) ([]byte, error) {
	pdf := newPDF()
	writeHeader(pdf, data.Garage, "QUOTE", data.Number)

	writeKeyValue(pdf, "Customer", data.CustomerName)
	writeKeyValue(pdf, "Vehicle", data.VehiclePlate)
	writeKeyValue(pdf, "Status", data.Status)

	writeLineTable(pdf, data.Lines, data.TotalAmount)

	if data.Notes != "" {
		pdf.Ln(4)
		pdf.SetFont(fontFamily, "B", 10)
		pdf.CellFormat(0, 6, "Notes", "", 1, "L", false, 0, "")
		pdf.SetFont(fontFamily, "", 10)
		pdf.MultiCell(0, 5, data.Notes, "", "L", false)
	}

	return finish(pdf)
}
