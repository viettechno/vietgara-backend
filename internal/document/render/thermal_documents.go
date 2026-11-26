package render

import (
	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// The roll templates of Release 13.0 (FR-DOC-06): the quick quote and the
// payment voucher on K80 and K57, in Vietnamese, for the counter.

// quoteRoll draws a quote on a roll: the letterhead, the customer, one block
// per line and the total.
func quoteRoll(c *canvas, q printmodel.Quote, o Options) error {
	if err := thermalLetterhead(c, q.Garage, o, "BẢN SAO"); err != nil {
		return err
	}

	if err := quoteHeading(c, q); err != nil {
		return err
	}

	c.feed(8)

	for _, f := range [][2]string{
		{"Khách hàng", q.CustomerName},
		{"Biển số", q.VehiclePlate},
		{"Đơn vị", q.BillTo.Name},
		{"MST", q.BillTo.TaxID},
		{"Địa chỉ", q.BillTo.Address},
	} {
		if err := c.field(f[0], f[1]); err != nil {
			return err
		}
	}

	c.rule()

	for _, line := range q.Lines {
		if err := c.text(line.Description, sizeBody, true, alignLeft); err != nil {
			return err
		}

		if err := c.pair(line.Quantity+" x "+formatVND(line.UnitPrice), formatVND(line.LineTotal), sizeBody, false); err != nil {
			return err
		}

		c.feed(4)
	}

	c.rule()

	if err := amount(c, "TỔNG CỘNG", q.TotalAmount); err != nil {
		return err
	}

	if q.Notes != "" {
		c.feed(8)

		if err := c.field("Ghi chú", q.Notes); err != nil {
			return err
		}
	}

	c.feed(16)

	return c.text("Cảm ơn quý khách!", sizeBody, false, alignCenter)
}

// paymentVoucherRoll draws a payment voucher (phiếu thu) on a roll: the
// letterhead, the payer, the amount in figures and in words, and the two
// signatures of the counter.
func paymentVoucherRoll(c *canvas, v printmodel.PaymentVoucher, o Options) error {
	if err := thermalLetterhead(c, v.Garage, o, "BẢN SAO"); err != nil {
		return err
	}

	if err := c.text("PHIẾU THU", sizeTitle, true, alignCenter); err != nil {
		return err
	}

	if err := c.text("Số: "+v.Number+" - Ngày "+v.Date, sizeBody, false, alignCenter); err != nil {
		return err
	}

	if v.Voided {
		if err := c.text("ĐÃ HỦY", sizeLarge, true, alignCenter); err != nil {
			return err
		}

		if err := c.field("Lý do hủy", v.VoidReason); err != nil {
			return err
		}
	}

	c.rule()

	for _, f := range [][2]string{
		{"Người nộp tiền", v.PayerName},
		{"Địa chỉ", v.PayerAddress},
		{"Lý do nộp", v.Reason},
		{"Hình thức", v.Method},
		{"Kèm theo", prefixed("Quyết toán ", v.SettlementNumber)},
	} {
		if err := c.field(f[0], f[1]); err != nil {
			return err
		}
	}

	c.rule()

	if err := amount(c, "SỐ TIỀN", v.Amount); err != nil {
		return err
	}

	if err := c.field("Bằng chữ", AmountInWords(v.Amount)); err != nil {
		return err
	}

	c.feed(24)

	if err := c.pair("Người nộp tiền", "Người thu tiền", sizeBody, true); err != nil {
		return err
	}

	if err := c.pair("(Ký, họ tên)", "(Ký, họ tên)", sizeSmall, false); err != nil {
		return err
	}

	c.feed(96)

	return c.text(v.CollectorName, sizeBody, false, alignRight)
}

// amount draws a label and, right aligned under it, the amount in large
// figures: both fit a K57 roll whatever the amount.
func amount(c *canvas, label string, value int64) error {
	if err := c.text(label, sizeBody, true, alignLeft); err != nil {
		return err
	}

	return c.text(formatVND(value), sizeLarge, true, alignRight)
}

// prefixed returns prefix + value, or "" for an empty value.
func prefixed(prefix, value string) string {
	if value == "" {
		return ""
	}

	return prefix + value
}

// quoteStatusLabel is the status a quote slip shows, so that a draft or a
// rejected quote is not taken for an agreed one; "" for an approved or
// converted quote, which the customer agreed to.
func quoteStatusLabel(status string) string {
	switch status {
	case "draft":
		return "(BẢN NHÁP)"
	case "sent":
		return "(CHỜ KHÁCH DUYỆT)"
	case "rejected":
		return "(KHÁCH ĐÃ TỪ CHỐI)"
	default:
		return ""
	}
}

// quoteHeading draws the title, the number and, unless the customer agreed
// to it, the quote's status.
func quoteHeading(c *canvas, q printmodel.Quote) error {
	if err := c.text("BÁO GIÁ", sizeTitle, true, alignCenter); err != nil {
		return err
	}

	if err := c.text("Số: "+q.Number, sizeBody, false, alignCenter); err != nil {
		return err
	}

	if label := quoteStatusLabel(q.Status); label != "" {
		return c.text(label, sizeBody, true, alignCenter)
	}

	return nil
}
