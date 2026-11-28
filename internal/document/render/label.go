package render

import (
	"bytes"
	"errors"
	"fmt"
	"image"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"golang.org/x/image/font"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// ErrNotLabelPaper is returned when labels are asked for on another paper.
var ErrNotLabelPaper = errors.New("render: labels print on label paper only")

// errLabelTooTall means the content of a label does not fit it; the layout
// keeps every line to one or two, so it is a bug in the layout, not data.
var errLabelTooTall = errors.New("render: the label's content is taller than the label")

// ErrBarcodeUnreadable is returned when a label's code is not ASCII, or too
// long for a Code 128 barcode a scanner reads (two dots a module) on the
// label.
var ErrBarcodeUnreadable = errors.New("render: the code does not fit a readable barcode on the label")

// labelDots is the height of a 50 x 30 mm label, in dots.
const labelDots = 240

// labelMargin is the blank edge of a label, in dots.
const labelMargin = 12

// PartLabels renders one label per entry of labels, in order, as ESC/POS for
// a label printer: each label is one image as tall as the label, followed by
// a feed to the next label (FR-INV-13).
func PartLabels(paper printmodel.Paper, labels []printmodel.PartLabel) ([]byte, error) {
	if paper != printmodel.Label5030 {
		return nil, ErrNotLabelPaper
	}

	var buf bytes.Buffer

	buf.Write(escInit)

	// Each distinct label is drawn once; its copies reuse the image.
	drawn := map[printmodel.PartLabel]*image.Gray{}

	for _, l := range labels {
		img, ok := drawn[l]
		if !ok {
			var err error
			if img, err = partLabel(l); err != nil {
				return nil, fmt.Errorf("label of %s: %w", l.SKU, err)
			}

			drawn[l] = img
		}

		raster(&buf, img, labelDots)
		buf.Write(escNextLabel)
	}

	return buf.Bytes(), nil
}

// partLabel draws one label: the name on at most two lines, the unit of a
// case label, the SKU and the barcode text on one line each, and the
// barcode, so that it always fits the label (checked: errLabelTooTall).
func partLabel(l printmodel.PartLabel) (*image.Gray, error) {
	inner, err := newCanvas(dotsWide(printmodel.Label5030) - 2*labelMargin)
	if err != nil {
		return nil, err
	}
	defer inner.close()

	name, err := inner.face(sizeBody, true)
	if err != nil {
		return nil, err
	}

	lines := wrap(name, l.Name, inner.width())
	if len(lines) > 2 {
		lines = append(lines[:1], ellipsis(name, lines[1], inner.width()))
	}

	for _, line := range lines {
		inner.line(name, sizeBody, line, "", alignLeft)
	}

	small, err := inner.face(sizeSmall, false)
	if err != nil {
		return nil, err
	}

	if l.Unit != "" {
		// The unit on a line of its own, so a long SKU never hides it.
		inner.line(name, sizeBody, oneLine(name, "["+l.Unit+"]", inner.width()), "", alignLeft)
	}

	inner.line(small, sizeSmall, oneLine(small, labelled("SKU", l.SKU), inner.width()), "", alignLeft)
	inner.feed(4)

	code, ok := barcodeImage(l.Barcode, inner.width(), 64)
	if !ok {
		return nil, ErrBarcodeUnreadable
	}

	inner.grow(inner.y + code.Rect.Dy())
	drawOnto(inner.img, code, (inner.width()-code.Rect.Dx())/2, inner.y, code.Rect.Dy())
	inner.y += code.Rect.Dy()

	inner.line(small, sizeSmall, oneLine(small, l.Barcode, inner.width()), "", alignCenter)

	if inner.y > labelDots-2*labelMargin {
		return nil, fmt.Errorf("%w: %d dots", errLabelTooTall, inner.y)
	}

	out := image.NewGray(image.Rect(0, 0, dotsWide(printmodel.Label5030), labelDots))
	for i := range out.Pix {
		out.Pix[i] = 0xff
	}

	drawOnto(out, inner.img, labelMargin, labelMargin, min(inner.y, labelDots-2*labelMargin))

	return out, nil
}

// oneLine is s, or s shortened with "…" when it is wider than width.
func oneLine(f font.Face, s string, width int) string {
	if font.MeasureString(f, s).Ceil() <= width {
		return s
	}

	return ellipsis(f, s, width)
}

// ellipsis shortens s with "…" until it fits width.
func ellipsis(f font.Face, s string, width int) string {
	runes := []rune(s)

	for len(runes) > 0 && font.MeasureString(f, string(runes)+"…").Ceil() > width {
		runes = runes[:len(runes)-1]
	}

	return string(runes) + "…"
}

// drawOnto copies the dark dots of the top height rows of src into dst, with
// its top left corner at x, y.
func drawOnto(dst, src *image.Gray, x, y, height int) {
	for sy := src.Rect.Min.Y; sy < min(src.Rect.Max.Y, src.Rect.Min.Y+height); sy++ {
		for sx := src.Rect.Min.X; sx < src.Rect.Max.X; sx++ {
			dx, dy := x+sx-src.Rect.Min.X, y+sy-src.Rect.Min.Y
			if image.Pt(dx, dy).In(dst.Rect) && src.GrayAt(sx, sy).Y < 0x80 {
				dst.SetGray(dx, dy, src.GrayAt(sx, sy))
			}
		}
	}
}

// barcodeImage draws code as a Code 128 barcode at most width dots wide and
// height dots tall, with whole-dot modules and a quiet zone of ten modules
// on each side. It is false when code cannot be encoded (a character outside
// ASCII) or does not fit at two dots a module: thinner bars (0.125 mm) do not
// scan reliably.
func barcodeImage(code string, width, height int) (*image.Gray, bool) {
	const quiet = 10

	bc, err := code128.Encode(code)
	if err != nil {
		return nil, false
	}

	modules := bc.Bounds().Dx()

	const minScale = 2

	scale := width / (modules + 2*quiet)
	if scale < minScale {
		return nil, false
	}

	img := image.NewGray(image.Rect(0, 0, modules*scale, height))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}

	for m := range modules {
		if isBar(bc, m) {
			fill(img, image.Rect(m*scale, 0, (m+1)*scale, height))
		}
	}

	return img, true
}

func isBar(bc barcode.Barcode, x int) bool {
	r, _, _, _ := bc.At(bc.Bounds().Min.X+x, bc.Bounds().Min.Y).RGBA()

	return r < 0x8000
}

// testText is the line of a test page that shows the printer prints every
// Vietnamese letter.
const testText = "Tiếng Việt: ă â đ ê ô ơ ư - Á À Ả Ã Ạ - Ứ Ừ Ử Ữ Ự"

// TestPageEscPos is a printer's test page as ESC/POS: a receipt on a roll or
// one label on label paper (FR-DOC-07).
func TestPageEscPos(paper printmodel.Paper, garageName, printerName string) ([]byte, error) {
	if paper == printmodel.Label5030 {
		return PartLabels(paper, []printmodel.PartLabel{{Name: printerName, SKU: "VIETGARA", Barcode: "VG-TEST"}})
	}

	c, err := newCanvas(dotsWide(paper))
	if err != nil {
		return nil, err
	}
	defer c.close()

	for _, l := range []struct {
		s    string
		size int
		bold bool
	}{
		{"TRANG IN THỬ", sizeTitle, true},
		{garageName, sizeLarge, true},
		{printerName, sizeBody, false},
		{"Khổ giấy " + string(paper), sizeBody, false},
		{testText, sizeBody, false},
	} {
		if err := c.text(l.s, l.size, l.bold, alignCenter); err != nil {
			return nil, err
		}
	}

	c.rule()
	c.feed(48)

	return c.receipt(), nil
}

// TestPagePDF is an IPP printer's test page on its page size, A4 when it
// has none (FR-DOC-07).
func TestPagePDF(paper printmodel.Paper, garageName, printerName string) ([]byte, error) {
	if !paper.IsPDF() {
		paper = printmodel.A4
	}

	pdf := newPDF(layoutFor(paper), "")

	pdf.SetFont(fontFamily, "B", 18)
	pdf.CellFormat(0, 10, "TRANG IN THỬ", "", 1, "C", false, 0, "")
	pdf.SetFont(fontFamily, "", 12)

	for _, line := range []string{garageName, printerName, testText} {
		pdf.MultiCell(0, 7, line, "", "C", false)
	}

	data, err := finish(pdf)
	if err != nil {
		return nil, fmt.Errorf("test page: %w", err)
	}

	return data, nil
}
