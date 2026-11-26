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

	for _, l := range labels {
		img, err := partLabel(l)
		if err != nil {
			return nil, err
		}

		raster(&buf, img, labelDots)
		buf.Write(escNextLabel)
	}

	return buf.Bytes(), nil
}

// partLabel draws one label: the name on at most two lines, the SKU, and the
// barcode with its text under it.
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

	if err := inner.text(labelled("SKU", l.SKU), sizeSmall, false, alignLeft); err != nil {
		return nil, err
	}

	inner.feed(4)

	if code, ok := barcodeImage(l.Barcode, inner.width(), 72); ok {
		inner.grow(inner.y + code.Rect.Dy())
		x := (inner.width() - code.Rect.Dx()) / 2
		drawOnto(inner.img, code, x, inner.y, code.Rect.Dy())
		inner.y += code.Rect.Dy()
	}

	if err := inner.text(l.Barcode, sizeSmall, false, alignCenter); err != nil {
		return nil, err
	}

	out := image.NewGray(image.Rect(0, 0, dotsWide(printmodel.Label5030), labelDots))
	for i := range out.Pix {
		out.Pix[i] = 0xff
	}

	drawOnto(out, inner.img, labelMargin, labelMargin, min(inner.y, labelDots-2*labelMargin))

	return out, nil
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
// ASCII) or does not fit even at one dot a module.
func barcodeImage(code string, width, height int) (*image.Gray, bool) {
	const quiet = 10

	bc, err := code128.Encode(code)
	if err != nil {
		return nil, false
	}

	modules := bc.Bounds().Dx()

	scale := width / (modules + 2*quiet)
	if scale < 1 {
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
		return PartLabels(paper, []printmodel.PartLabel{{Name: printerName, SKU: "VIETGARA", Barcode: "VIETGARA-TEST"}})
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

// TestPagePDF is an IPP printer's test page, on A4 (FR-DOC-07).
func TestPagePDF(garageName, printerName string) ([]byte, error) {
	pdf := newPDF(layoutFor(printmodel.A4), "")

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
