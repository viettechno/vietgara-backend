package render

import (
	"github.com/go-pdf/fpdf"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// paymentVoucherTemplate renders a payment voucher (phiếu thu) in the 01-TT
// layout of Circular 200/2014/TT-BTC: the garage's legal header, the payer,
// the reason, the amount in figures and in words, and signature blocks.
func paymentVoucherTemplate(data printmodel.PaymentVoucher, o Options) ([]byte, error) {
	lay := layoutFor(o.Paper)
	pdf := newPDF(lay, copyLabel(o, "BẢN SAO"))

	writeLetterhead(pdf, lay, data.Garage)
	pdf.Ln(2)

	pdf.SetFont(fontFamily, "", 8)
	pdf.MultiCell(0, 4, "Mẫu số 01 - TT\n(Ban hành theo Thông tư số 200/2014/TT-BTC ngày 22/12/2014 của Bộ Tài chính)", "", "R", false)
	pdf.Ln(2)

	pdf.SetFont(fontFamily, "B", 16)
	pdf.CellFormat(0, 9, "PHIẾU THU", "", 1, "C", false, 0, "")
	pdf.SetFont(fontFamily, "", 10)
	pdf.CellFormat(0, 6, "Ngày "+data.Date, "", 1, "C", false, 0, "")
	pdf.CellFormat(0, 6, "Số: "+data.Number, "", 1, "C", false, 0, "")

	if data.Voided {
		pdf.SetFont(fontFamily, "B", 12)
		pdf.SetTextColor(180, 0, 0)
		pdf.CellFormat(0, 7, "ĐÃ HỦY", "", 1, "C", false, 0, "")
		pdf.SetFont(fontFamily, "", 10)
		pdf.MultiCell(0, 5, "Lý do hủy: "+data.VoidReason, "", "C", false)
		pdf.SetTextColor(0, 0, 0)
	}

	pdf.Ln(4)

	writeField(pdf, lay, "Họ và tên người nộp tiền", data.PayerName)
	writeField(pdf, lay, "Địa chỉ", data.PayerAddress)
	writeField(pdf, lay, "Lý do nộp", data.Reason)
	writeField(pdf, lay, "Số tiền", formatVND(data.Amount))
	writeField(pdf, lay, "Viết bằng chữ", AmountInWords(data.Amount))
	writeField(pdf, lay, "Hình thức", data.Method)
	writeField(pdf, lay, "Kèm theo", "Quyết toán "+data.SettlementNumber)

	writeSignatures(pdf, lay, data.CollectorName)

	return finish(pdf)
}

// writeField prints one "label: value" line of the voucher, wrapping a long
// value.
func writeField(pdf *fpdf.Fpdf, lay layout, label, value string) {
	pdf.SetFont(fontFamily, "B", 10)
	pdf.CellFormat(lay.fieldLabel, 6, label+":", "", 0, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 10)
	pdf.MultiCell(0, 6, value, "", "L", false)
}

// writeSignatures prints the 01-TT signature blocks; the collector's name
// is printed under "Thủ quỹ".
func writeSignatures(pdf *fpdf.Fpdf, lay layout, collector string) {
	pdf.Ln(8)

	roles := [...]string{"Giám đốc", "Kế toán trưởng", "Người nộp tiền", "Người lập phiếu", "Thủ quỹ"}
	width := lay.content / float64(len(roles))

	pdf.SetFont(fontFamily, "B", 9)

	for _, role := range roles {
		pdf.CellFormat(width, 5, role, "", 0, "C", false, 0, "")
	}

	pdf.Ln(-1)
	pdf.SetFont(fontFamily, "", 8)

	for range roles {
		pdf.CellFormat(width, 5, "(Ký, họ tên)", "", 0, "C", false, 0, "")
	}

	pdf.Ln(-1)
	pdf.Ln(18)

	pdf.SetFont(fontFamily, "", 9)

	for _, role := range roles {
		name := ""
		if role == "Thủ quỹ" {
			name = collector
		}

		pdf.CellFormat(width, 5, name, "", 0, "C", false, 0, "")
	}

	pdf.Ln(-1)
}
