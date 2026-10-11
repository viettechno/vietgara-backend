// Package render turns plain data structs into PDF/Excel bytes (FRD
// 3.13). It has no database or use-case dependencies and knows nothing
// about any other module -- the document use case reads the data through
// its own narrow reader interfaces and hands this package only what a
// layout needs, the same cross-module decoupling pattern used everywhere
// else (HLD Section 2). No templates are stored (FR-DOC-03, customizing
// the logo and layout, is Phase 4): the layout below is the garage's one
// standard layout for every document of its kind.
package render

import (
	"bytes"
	_ "embed" // the PDF fonts
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-pdf/fpdf"
)

// GarageHeader is the letterhead shown at the top of every PDF: the
// garage's legal identity (FR-TEN-06), with its logo when it has one.
type GarageHeader struct {
	// Name is the legal name, or the trading name when none is set.
	Name string
	// Address is the legal address, or the trading address.
	Address string
	Phone   string
	// TaxCode, Representative and Bank are printed when set; Bank is one
	// line such as "Vietcombank - 0011 2233 - Cong ty An Phat".
	TaxCode        string
	Representative string
	Bank           string
	// Logo is the image content (PNG or JPEG); empty prints no logo.
	Logo []byte
}

// BillTo is who a document is billed to (FR-CUS-07), printed under the
// header when the document was billed to a tax profile.
type BillTo struct {
	Name    string
	TaxID   string
	Address string
}

// IsZero reports whether the document is a retail receipt.
func (b BillTo) IsZero() bool { return b.Name == "" }

// writeBillTo prints the party the document is billed to, if any.
func writeBillTo(pdf *fpdf.Fpdf, b BillTo) {
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

// DocumentLine is one line item row (a quote line, a repair order line).
type DocumentLine struct {
	Description string
	Quantity    string
	UnitPrice   int64
	LineTotal   int64
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

func newPDF() *fpdf.Fpdf {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.AddUTF8FontFromBytes(fontFamily, "", fontRegular)
	pdf.AddUTF8FontFromBytes(fontFamily, "B", fontBold)
	pdf.SetMargins(18, 18, 18)
	pdf.AddPage()
	pdf.SetFont(fontFamily, "", 10)

	return pdf
}

// logoHeight is the height of the logo in the header, in mm.
const logoHeight = 18

// drawLogo puts the logo at the top right of the page. An image the PDF
// library cannot read (a WebP, a corrupt file) is left out: a document
// without a logo is better than no document.
func drawLogo(pdf *fpdf.Fpdf, logo []byte) {
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
	pdf.ImageOptions("logo", pageWidth-18-width, 18, width, logoHeight, false, fpdf.ImageOptions{ImageType: imageType}, 0, "")
}

// writeLetterhead prints the logo and the garage's legal identity.
func writeLetterhead(pdf *fpdf.Fpdf, garage GarageHeader) {
	if len(garage.Logo) > 0 {
		drawLogo(pdf, garage.Logo)
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

func writeHeader(pdf *fpdf.Fpdf, garage GarageHeader, title, number string) {
	writeLetterhead(pdf, garage)

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

var lineColumnWidths = [4]float64{90, 25, 35, 35}

func writeLineTable(pdf *fpdf.Fpdf, lines []DocumentLine, total int64) {
	pdf.Ln(2)
	pdf.SetFont(fontFamily, "B", 10)
	pdf.SetFillColor(240, 240, 240)

	headers := [4]string{"Description", "Qty", "Unit Price", "Line Total"}

	for i, h := range headers {
		pdf.CellFormat(lineColumnWidths[i], 7, h, "1", 0, "L", true, 0, "")
	}

	pdf.Ln(-1)

	pdf.SetFont(fontFamily, "", 10)

	for _, line := range lines {
		pdf.CellFormat(lineColumnWidths[0], 7, line.Description, "1", 0, "L", false, 0, "")
		pdf.CellFormat(lineColumnWidths[1], 7, line.Quantity, "1", 0, "R", false, 0, "")
		pdf.CellFormat(lineColumnWidths[2], 7, formatVND(line.UnitPrice), "1", 0, "R", false, 0, "")
		pdf.CellFormat(lineColumnWidths[3], 7, formatVND(line.LineTotal), "1", 0, "R", false, 0, "")
		pdf.Ln(-1)
	}

	pdf.SetFont(fontFamily, "B", 10)
	pdf.CellFormat(lineColumnWidths[0]+lineColumnWidths[1]+lineColumnWidths[2], 8, "Total", "1", 0, "R", false, 0, "")
	pdf.CellFormat(lineColumnWidths[3], 8, formatVND(total), "1", 0, "R", false, 0, "")
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
