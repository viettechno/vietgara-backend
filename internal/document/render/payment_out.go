package render

import (
	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// paymentOutTemplate renders a payment-out voucher (phiếu chi) in the 02-TT
// layout of Circular 200/2014/TT-BTC: the garage's legal header, the payee,
// the reason, the amount in figures and in words, the receipts it pays and
// the signature blocks.
func paymentOutTemplate(data printmodel.PaymentOutVoucher, o Options) ([]byte, error) {
	lay := layoutFor(o.Paper)
	pdf := newPDF(lay, copyLabel(o, "BẢN SAO"))

	writeLetterhead(pdf, lay, data.Garage)
	pdf.Ln(2)

	pdf.SetFont(fontFamily, "", 8)
	pdf.MultiCell(0, 4, "Mẫu số 02 - TT\n(Ban hành theo Thông tư số 200/2014/TT-BTC ngày 22/12/2014 của Bộ Tài chính)", "", "R", false)
	pdf.Ln(2)

	pdf.SetFont(fontFamily, "B", 16)
	pdf.CellFormat(0, 9, "PHIẾU CHI", "", 1, "C", false, 0, "")
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

	writeField(pdf, lay, "Họ và tên người nhận tiền", data.PayeeName)
	writeField(pdf, lay, "Địa chỉ", data.PayeeAddress)
	writeField(pdf, lay, "Lý do chi", data.Reason)
	writeField(pdf, lay, "Số tiền", formatVND(data.Amount))
	writeField(pdf, lay, "Viết bằng chữ", AmountInWords(data.Amount))
	writeField(pdf, lay, "Hình thức", data.Method)

	for _, r := range data.Receipts {
		writeField(pdf, lay, "Kèm theo", "Phiếu nhập "+r.Number+": "+formatVND(r.Amount))
	}

	writeSignatures(pdf, lay, paymentOutRoles[:], data.IssuerName, "Người lập phiếu")

	return finish(pdf)
}

// paymentOutRoles are the signature blocks of the 02-TT voucher, left to right.
var paymentOutRoles = [...]string{"Giám đốc", "Kế toán trưởng", "Thủ quỹ", "Người lập phiếu", "Người nhận tiền"}
