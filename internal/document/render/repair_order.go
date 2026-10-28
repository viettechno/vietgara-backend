package render

import "github.com/viettechno/vietgara-backend/internal/document/printmodel"

// repairOrderTemplate renders a repair order (FR-DOC-01).
func repairOrderTemplate(data printmodel.RepairOrder, o Options) ([]byte, error) {
	lay := layoutFor(o.Paper)
	pdf := newPDF(lay, copyLabel(o, "COPY"))
	writeHeader(pdf, lay, data.Garage, "REPAIR ORDER", data.Number)

	writeBillTo(pdf, data.BillTo)
	writeKeyValue(pdf, "Customer", data.CustomerName)
	writeKeyValue(pdf, "Vehicle", data.VehiclePlate)
	writeKeyValue(pdf, "Status", data.Status)

	writeLineTable(pdf, lay, data.Lines, data.TotalAmount)

	return finish(pdf)
}
