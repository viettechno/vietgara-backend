package render

// PaymentRow is one payment line on a settlement receipt.
type PaymentRow struct {
	Date      string
	PayerType string
	Method    string
	Amount    int64
}

// SettlementReceiptData is everything a settlement receipt's PDF needs
// (FR-DOC-01).
type SettlementReceiptData struct {
	Garage         GarageHeader
	Number         string
	Status         string
	CustomerName   string
	VehiclePlate   string
	TotalAmount    int64
	CustomerAmount int64
	InsurerAmount  int64
	InsurerName    string
	Payments       []PaymentRow
}

// SettlementReceiptPDF renders a settlement as a receipt PDF using
// the garage's standard layout.
func SettlementReceiptPDF(data SettlementReceiptData) ([]byte, error) {
	pdf := newPDF()
	writeHeader(pdf, data.Garage, "RECEIPT", data.Number)

	writeKeyValue(pdf, "Customer", data.CustomerName)
	writeKeyValue(pdf, "Vehicle", data.VehiclePlate)
	writeKeyValue(pdf, "Status", data.Status)
	writeKeyValue(pdf, "Customer share", formatVND(data.CustomerAmount))

	if data.InsurerAmount > 0 {
		writeKeyValue(pdf, "Insurer share", formatVND(data.InsurerAmount)+" ("+data.InsurerName+")")
	}

	pdf.Ln(4)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(0, 7, "Total: "+formatVND(data.TotalAmount), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(0, 6, "Payments received", "", 1, "L", false, 0, "")

	if len(data.Payments) == 0 {
		pdf.SetFont("Helvetica", "", 10)
		pdf.CellFormat(0, 6, "None yet.", "", 1, "L", false, 0, "")

		return finish(pdf)
	}

	widths := [4]float64{40, 35, 40, 70}
	headers := [4]string{"Date", "Payer", "Method", "Amount"}

	pdf.SetFillColor(240, 240, 240)

	for i, h := range headers {
		pdf.CellFormat(widths[i], 7, h, "1", 0, "L", true, 0, "")
	}

	pdf.Ln(-1)

	pdf.SetFont("Helvetica", "", 10)

	for _, p := range data.Payments {
		pdf.CellFormat(widths[0], 7, p.Date, "1", 0, "L", false, 0, "")
		pdf.CellFormat(widths[1], 7, p.PayerType, "1", 0, "L", false, 0, "")
		pdf.CellFormat(widths[2], 7, p.Method, "1", 0, "L", false, 0, "")
		pdf.CellFormat(widths[3], 7, formatVND(p.Amount), "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}

	return finish(pdf)
}
