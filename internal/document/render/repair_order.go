package render

// RepairOrderPDFData is everything a repair order's PDF needs (FR-DOC-01).
type RepairOrderPDFData struct {
	Garage       GarageHeader
	BillTo       BillTo
	Number       string
	Status       string
	CustomerName string
	VehiclePlate string
	Lines        []DocumentLine
	TotalAmount  int64
}

// RepairOrderPDF renders a repair order as a PDF using the garage's
// standard layout.
func RepairOrderPDF(data RepairOrderPDFData) ([]byte, error) {
	pdf := newPDF()
	writeHeader(pdf, data.Garage, "REPAIR ORDER", data.Number)

	writeBillTo(pdf, data.BillTo)
	writeKeyValue(pdf, "Customer", data.CustomerName)
	writeKeyValue(pdf, "Vehicle", data.VehiclePlate)
	writeKeyValue(pdf, "Status", data.Status)

	writeLineTable(pdf, data.Lines, data.TotalAmount)

	return finish(pdf)
}
