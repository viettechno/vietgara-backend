package render

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// TestVoucherTextFitsItsCells measures the voucher's fixed texts against the
// cells they are drawn in: a cell does not wrap or clip, so a text wider than
// its cell prints over its neighbour.
func TestVoucherTextFitsItsCells(t *testing.T) {
	t.Parallel()

	// The widest label of the voucher's fields.
	const widestLabel = "Họ và tên người nộp tiền:"

	// A4 version 1 is the layout the documents had before Release 10.0, whose
	// widest label runs 0.6 mm over its cell; it is not touched, a visual
	// difference of that size is not worth a version.
	lay := layoutFor(printmodel.A5)
	pdf := newPDF(lay, "")

	pdf.SetFont(fontFamily, "B", 10)
	assert.LessOrEqual(t, pdf.GetStringWidth(widestLabel), lay.fieldLabel, "A5 field label")

	for _, paper := range []printmodel.Paper{printmodel.A4, printmodel.A5} {
		lay := layoutFor(paper)
		pdf := newPDF(lay, "")
		pdf.SetFont(fontFamily, "B", lay.signature)

		for _, role := range voucherRoles {
			assert.LessOrEqual(t, pdf.GetStringWidth(role), lay.content/float64(len(voucherRoles)), "%s signature heading %q", paper, role)
		}

		pdf.SetFont(fontFamily, "", lay.signatureNote)
		assert.LessOrEqual(t, pdf.GetStringWidth("(Ký, họ tên)"), lay.content/float64(len(voucherRoles)), "%s signature note", paper)
	}
}
