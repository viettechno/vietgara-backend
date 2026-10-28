// Package render turns print models into PDF and Excel bytes (FRD 3.13). It
// has no database or use-case dependencies and knows nothing about any other
// module -- the document use case reads the data through its own narrow
// reader interfaces, builds the print model (package printmodel) and picks a
// template from the registry (registry.go). A template reads the model and
// nothing else.
package render

import (
	"bytes"
	_ "embed" // the PDF fonts
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-pdf/fpdf"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// Options are what a template needs besides the model.
type Options struct {
	// Paper is the paper size the template was looked up for.
	Paper printmodel.Paper
	// Copy prints the copy mark: the document was printed before.
	Copy bool
}

// layout holds the measures of one paper size, in mm. The A4 values are the
// layout every document had before Release 10.0 (template version 1).
type layout struct {
	paper  printmodel.Paper
	margin float64
	// content is the printable width.
	content float64
	// fieldLabel is the label width of a voucher field.
	fieldLabel float64
	// lines are the column widths of the line table: description, quantity,
	// unit price, line total.
	lines [4]float64
	// payments are the columns of the payments table of a receipt.
	payments [3]float64
	// goods are the columns of a goods receipt: SKU, part, quantity,
	// supplier, unit cost, amount.
	goods [6]float64
	// stock are the columns of a stock issue: SKU, part, quantity.
	stock [3]float64
}

func layoutFor(paper printmodel.Paper) layout {
	if paper == printmodel.A5 {
		return layout{
			paper: paper, margin: 10, content: 128, fieldLabel: 42,
			lines:    [4]float64{58, 16, 27, 27},
			payments: [3]float64{30, 38, 60},
			goods:    [6]float64{16, 34, 14, 22, 20, 22},
			stock:    [3]float64{26, 70, 32},
		}
	}

	return layout{
		paper: printmodel.A4, margin: 18, content: 174, fieldLabel: 50,
		lines:    [4]float64{90, 25, 35, 35},
		payments: [3]float64{50, 60, 75},
		goods:    [6]float64{24, 52, 22, 30, 24, 22},
		stock:    [3]float64{35, 100, 40},
	}
}

// fontFamily is the embedded TrueType family every document uses: the PDF
// core fonts are Latin-1 only and cannot print Vietnamese diacritics.
// DejaVu Sans is free to embed (fonts/LICENSE).
const fontFamily = "DejaVu"

var (
	//go:embed fonts/DejaVuSans.ttf
	fontRegular []byte
	//go:embed fonts/DejaVuSans-Bold.ttf
	fontBold []byte
)

// newPDF starts a document on lay's paper. A non-empty copyLabel is printed
// at the top right of every page.
func newPDF(lay layout, copyLabel string) *fpdf.Fpdf {
	pdf := fpdf.New("P", "mm", string(lay.paper), "")
	pdf.AddUTF8FontFromBytes(fontFamily, "", fontRegular)
	pdf.AddUTF8FontFromBytes(fontFamily, "B", fontBold)
	pdf.SetMargins(lay.margin, lay.margin, lay.margin)

	if copyLabel != "" {
		pdf.SetHeaderFunc(func() { drawCopyMark(pdf, lay, copyLabel) })
	}

	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 10)

	return pdf
}

// copyMarkWidth is the width of the copy mark's box, in mm.
const copyMarkWidth = 28

// drawCopyMark prints the copy mark above the top right corner of the text
// area.
func drawCopyMark(pdf *fpdf.Fpdf, lay layout, label string) {
	pageWidth, _ := pdf.GetPageSize()

	pdf.SetXY(pageWidth-lay.margin-copyMarkWidth, lay.margin/3)
	pdf.SetFont(fontFamily, "B", 9)
	pdf.SetTextColor(180, 0, 0)
	pdf.SetDrawColor(180, 0, 0)
	pdf.CellFormat(copyMarkWidth, 5, label, "1", 0, "C", false, 0, "")
	pdf.SetTextColor(0, 0, 0)
	pdf.SetDrawColor(0, 0, 0)
	pdf.SetFont(fontFamily, "", 10)
	pdf.SetXY(lay.margin, lay.margin)
}

// writeBillTo prints the party the document is billed to, if any.
func writeBillTo(pdf *fpdf.Fpdf, b printmodel.BillTo) {
	if b.IsZero() {
		return
	}

	writeKeyValue(pdf, "Bill to", b.Name)

	if b.TaxID != "" {
		writeKeyValue(pdf, "Tax ID", b.TaxID)
	}

	if b.Address != "" {
		writeKeyValue(pdf, "Address", b.Address)
	}
}

// logoHeight is the height of the logo in the header, in mm.
const logoHeight = 18

// drawLogo puts the logo at the top right of the page. An image the PDF
// library cannot read (a WebP, a corrupt file) is left out: a document
// without a logo is better than no document.
func drawLogo(pdf *fpdf.Fpdf, lay layout, logo []byte) {
	var imageType string

	switch http.DetectContentType(logo) {
	case "image/png":
		imageType = "PNG"
	case "image/jpeg":
		imageType = "JPG"
	default:
		return
	}

	pdf.RegisterImageOptionsReader("logo", fpdf.ImageOptions{ImageType: imageType, ReadDpi: false}, bytes.NewReader(logo))

	if pdf.Error() != nil {
		pdf.ClearError()

		return
	}

	info := pdf.GetImageInfo("logo")
	if info == nil {
		return
	}

	width := logoHeight * info.Width() / info.Height()
	pageWidth, _ := pdf.GetPageSize()
	pdf.ImageOptions("logo", pageWidth-lay.margin-width, lay.margin, width, logoHeight, false, fpdf.ImageOptions{ImageType: imageType}, 0, "")
}

// writeLetterhead prints the logo and the garage's legal identity.
func writeLetterhead(pdf *fpdf.Fpdf, lay layout, garage printmodel.GarageHeader) {
	if len(garage.Logo) > 0 {
		drawLogo(pdf, lay, garage.Logo)
	}

	pdf.SetFont(fontFamily, "B", 16)
	pdf.CellFormat(0, 8, garage.Name, "", 1, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 10)

	for _, line := range []string{
		garage.Address, labelled("Tax code", garage.TaxCode), garage.Phone,
		labelled("Representative", garage.Representative), labelled("Bank", garage.Bank),
	} {
		if line != "" {
			pdf.CellFormat(0, 5, line, "", 1, "L", false, 0, "")
		}
	}
}

func writeHeader(pdf *fpdf.Fpdf, lay layout, garage printmodel.GarageHeader, title, number string) {
	writeLetterhead(pdf, lay, garage)

	pdf.Ln(4)
	pdf.SetFont(fontFamily, "B", 14)
	pdf.CellFormat(0, 8, title, "", 1, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 10)
	pdf.CellFormat(0, 6, "No. "+number, "", 1, "L", false, 0, "")
	pdf.Ln(4)
}

// labelled returns "label: value", or "" for an empty value.
func labelled(label, value string) string {
	if value == "" {
		return ""
	}

	return label + ": " + value
}

func writeKeyValue(pdf *fpdf.Fpdf, label, value string) {
	pdf.SetFont(fontFamily, "B", 10)
	pdf.CellFormat(40, 6, label, "", 0, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 10)
	pdf.CellFormat(0, 6, value, "", 1, "L", false, 0, "")
}

func writeLineTable(pdf *fpdf.Fpdf, lay layout, lines []printmodel.DocumentLine, total int64) {
	widths := lay.lines

	pdf.Ln(2)
	pdf.SetFont(fontFamily, "B", 10)
	pdf.SetFillColor(240, 240, 240)

	headers := [4]string{"Description", "Qty", "Unit Price", "Line Total"}

	for i, h := range headers {
		pdf.CellFormat(widths[i], 7, h, "1", 0, "L", true, 0, "")
	}

	pdf.Ln(-1)

	pdf.SetFont(fontFamily, "", 10)

	for _, line := range lines {
		pdf.CellFormat(widths[0], 7, line.Description, "1", 0, "L", false, 0, "")
		pdf.CellFormat(widths[1], 7, line.Quantity, "1", 0, "R", false, 0, "")
		pdf.CellFormat(widths[2], 7, formatVND(line.UnitPrice), "1", 0, "R", false, 0, "")
		pdf.CellFormat(widths[3], 7, formatVND(line.LineTotal), "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}

	pdf.SetFont(fontFamily, "B", 10)
	pdf.CellFormat(widths[0]+widths[1]+widths[2], 8, labelTotal, "1", 0, "R", false, 0, "")
	pdf.CellFormat(widths[3], 8, formatVND(total), "1", 0, "R", false, 0, "")
	pdf.Ln(-1)
}

func finish(pdf *fpdf.Fpdf) ([]byte, error) {
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("render pdf: %w", err)
	}

	return buf.Bytes(), nil
}

// formatVND renders an integer VND amount with thousands separators,
// e.g. 1234567 -> "1,234,567 VND".
func formatVND(amount int64) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}

	digits := strconv.FormatInt(amount, 10)
	grouped := make([]byte, 0, len(digits)+len(digits)/3)

	for i, d := range []byte(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped = append(grouped, ',')
		}

		grouped = append(grouped, d)
	}

	sign := ""
	if negative {
		sign = "-"
	}

	return sign + string(grouped) + " VND"
}
