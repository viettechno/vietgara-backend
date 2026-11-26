package render

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
)

// The thermal output (Release 13.0) is ESC/POS raster graphics: every line is
// drawn with the embedded DejaVu font into a 1-bit image, so Vietnamese
// diacritics print on any ESC/POS printer, whatever code pages it has. A
// thermal printer has 8 dots per mm (203 dpi).

// dotsWide is the printed width in dots of a roll or a label.
func dotsWide(paper printmodel.Paper) int {
	switch paper {
	case printmodel.K57:
		return 384 // 48 mm
	case printmodel.Label5030:
		return 400 // 50 mm
	case printmodel.K80, printmodel.A4, printmodel.A5:
		return 576 // 72 mm; a page size is not drawn on a canvas
	default:
		return 576
	}
}

// The ESC/POS commands the output uses.
var (
	escInit = []byte{0x1b, 0x40}             // ESC @: reset the printer
	escCut  = []byte{0x1d, 0x56, 0x42, 0x00} // GS V B 0: feed to the cutter and cut partly
	// escNextLabel is GS FF: feed to the start of the next label.
	escNextLabel = []byte{0x1d, 0x0c}
)

// bandRows is the height of one raster command: printers with a small buffer
// refuse a whole receipt in one image.
const bandRows = 128

// The font sizes, in dots (a dot is 1/8 mm).
const (
	sizeSmall = 20
	sizeBody  = 24
	sizeLarge = 30
	sizeTitle = 36
)

type align int

const (
	alignLeft align = iota
	alignCenter
	alignRight
)

// thermalFonts are the parsed embedded fonts, regular and bold.
var thermalFonts = sync.OnceValues(func() ([2]*opentype.Font, error) {
	regular, err := opentype.Parse(fontRegular)
	if err != nil {
		return [2]*opentype.Font{}, fmt.Errorf("parse regular font: %w", err)
	}

	bold, err := opentype.Parse(fontBold)
	if err != nil {
		return [2]*opentype.Font{}, fmt.Errorf("parse bold font: %w", err)
	}

	return [2]*opentype.Font{regular, bold}, nil
})

type faceKey struct {
	size int
	bold bool
}

// canvas is a receipt or a label being drawn: a white image as wide as the
// paper that grows downwards. A canvas is not safe for concurrent use.
type canvas struct {
	img   *image.Gray
	y     int
	fonts [2]*opentype.Font
	faces map[faceKey]font.Face
}

func newCanvas(width int) (*canvas, error) {
	fonts, err := thermalFonts()
	if err != nil {
		return nil, err
	}

	c := &canvas{img: image.NewGray(image.Rect(0, 0, width, 0)), fonts: fonts, faces: map[faceKey]font.Face{}}

	return c, nil
}

func (c *canvas) width() int { return c.img.Rect.Dx() }

// grow makes the image at least height dots tall, the new rows white.
func (c *canvas) grow(height int) {
	if height <= c.img.Rect.Dy() {
		return
	}

	bigger := image.NewGray(image.Rect(0, 0, c.width(), max(height, 2*c.img.Rect.Dy())))
	draw.Draw(bigger, bigger.Rect, image.White, image.Point{}, draw.Src)
	draw.Draw(bigger, c.img.Rect, c.img, image.Point{}, draw.Src)
	c.img = bigger
}

func (c *canvas) face(size int, bold bool) (font.Face, error) {
	key := faceKey{size, bold}
	if f, ok := c.faces[key]; ok {
		return f, nil
	}

	fnt := c.fonts[0]
	if bold {
		fnt = c.fonts[1]
	}

	f, err := opentype.NewFace(fnt, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, fmt.Errorf("font face: %w", err)
	}

	c.faces[key] = f

	return f, nil
}

func (c *canvas) close() {
	for _, f := range c.faces {
		_ = f.Close()
	}
}

// feed leaves n blank dots.
func (c *canvas) feed(n int) {
	c.y += n
	c.grow(c.y)
}

// text draws s, wrapped to the width, one or more lines.
func (c *canvas) text(s string, size int, bold bool, a align) error {
	f, err := c.face(size, bold)
	if err != nil {
		return err
	}

	for _, line := range wrap(f, s, c.width()) {
		c.line(f, size, line, "", a)
	}

	return nil
}

// pair draws left at the left and right at the right of one line, left
// wrapped into the room right leaves; its next lines are left alone. When
// right leaves less than a third of the width, it goes on a line of its own.
func (c *canvas) pair(left, right string, size int, bold bool) error {
	f, err := c.face(size, bold)
	if err != nil {
		return err
	}

	room := c.width() - font.MeasureString(f, right).Ceil() - size/2
	if room < c.width()/3 {
		// Too little room beside right: left on its lines, right under it.
		for _, line := range wrap(f, left, c.width()) {
			c.line(f, size, line, "", alignLeft)
		}

		c.line(f, size, right, "", alignRight)

		return nil
	}

	lines := wrap(f, left, room)

	for i, line := range lines {
		r := ""
		if i == 0 {
			r = right
		}

		c.line(f, size, line, r, alignLeft)
	}

	return nil
}

// field draws "label: value", wrapped; an empty value draws nothing.
func (c *canvas) field(label, value string) error {
	if value == "" {
		return nil
	}

	return c.text(label+": "+value, sizeBody, false, alignLeft)
}

// line draws one line of text, and right aligned to the right edge when set.
// The line is as tall as the font's ascent and descent, or as the ink of
// its text when that reaches further, so stacked Vietnamese marks (Ẫ, Ứ)
// and dots below (ạ, ự) stay inside it.
func (c *canvas) line(f font.Face, _ int, s, right string, a align) {
	metrics := f.Metrics()
	above, below := metrics.Ascent.Ceil(), metrics.Descent.Ceil()

	for _, text := range []string{s, right} {
		bounds, _ := font.BoundString(f, text)
		above, below = max(above, (-bounds.Min.Y).Ceil()), max(below, bounds.Max.Y.Ceil())
	}

	height := above + below
	c.grow(c.y + height)

	baseline := c.y + above

	x := 0

	switch a {
	case alignCenter:
		x = (c.width() - font.MeasureString(f, s).Ceil()) / 2
	case alignRight:
		x = c.width() - font.MeasureString(f, s).Ceil()
	case alignLeft:
	}

	c.draw(f, s, max(x, 0), baseline)

	if right != "" {
		c.draw(f, right, c.width()-font.MeasureString(f, right).Ceil(), baseline)
	}

	c.y += height
}

func (c *canvas) draw(f font.Face, s string, x, baseline int) {
	d := font.Drawer{Dst: c.img, Src: image.Black, Face: f, Dot: fixed.P(x, baseline)}
	d.DrawString(s)
}

// rule draws a dashed line across.
func (c *canvas) rule() {
	const pad, dash, thick = 8, 8, 2

	c.grow(c.y + 2*pad + thick)

	for x := 0; x < c.width(); x += 2 * dash {
		fill(c.img, image.Rect(x, c.y+pad, min(x+dash, c.width()), c.y+pad+thick))
	}

	c.y += 2*pad + thick
}

// box draws a frame around the text, centered: the copy mark.
func (c *canvas) box(s string) error {
	f, err := c.face(sizeLarge, true)
	if err != nil {
		return err
	}

	const pad, thick = 8, 3

	w := font.MeasureString(f, s).Ceil() + 2*pad
	x0 := (c.width() - w) / 2
	top := c.y
	c.line(f, sizeLarge, s, "", alignCenter)
	bottom := c.y

	for _, r := range []image.Rectangle{
		image.Rect(x0, top, x0+w, top+thick), image.Rect(x0, bottom-thick, x0+w, bottom),
		image.Rect(x0, top, x0+thick, bottom), image.Rect(x0+w-thick, top, x0+w, bottom),
	} {
		fill(c.img, r)
	}

	c.feed(8)

	return nil
}

func fill(img *image.Gray, r image.Rectangle) {
	draw.Draw(img, r.Intersect(img.Rect), image.Black, image.Point{}, draw.Src)
}

// wrap splits s into lines no wider than width, at spaces where it can and
// inside a word that is wider than a line. An empty s is no line.
func wrap(f font.Face, s string, width int) []string {
	var lines []string

	for paragraph := range strings.SplitSeq(s, "\n") {
		current := ""

		for word := range strings.FieldsSeq(paragraph) {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}

			if font.MeasureString(f, candidate).Ceil() <= width {
				current = candidate

				continue
			}

			if current != "" {
				lines = append(lines, current)
			}

			current = ""

			for _, r := range word {
				if current != "" && font.MeasureString(f, current+string(r)).Ceil() > width {
					lines = append(lines, current)
					current = ""
				}

				current += string(r)
			}
		}

		if current != "" {
			lines = append(lines, current)
		}
	}

	return lines
}

// raster appends img as ESC/POS raster graphics (GS v 0), in bands of
// bandRows. A pixel darker than mid-grey prints.
func raster(buf *bytes.Buffer, img *image.Gray, height int) {
	width := img.Rect.Dx()
	rowBytes := (width + 7) / 8

	for top := 0; top < height; top += bandRows {
		rows := min(bandRows, height-top)

		//nolint:gosec // rowBytes is at most 72 and rows at most bandRows
		buf.Write([]byte{0x1d, 0x76, 0x30, 0x00, byte(rowBytes), byte(rowBytes >> 8), byte(rows), byte(rows >> 8)})

		for y := top; y < top+rows; y++ {
			for xb := range rowBytes {
				var b byte

				for bit := range 8 {
					x := xb*8 + bit
					if x < width && img.GrayAt(x, y).Y < 0x80 {
						b |= 0x80 >> bit
					}
				}

				buf.WriteByte(b)
			}
		}
	}
}

// receipt is the ESC/POS of a drawn roll: the image, a feed and a cut.
func (c *canvas) receipt() []byte {
	var buf bytes.Buffer

	buf.Write(escInit)
	raster(&buf, c.img, c.y)
	buf.Write(escCut)

	return buf.Bytes()
}

// thermalTemplate wraps a roll template: it draws on a canvas as wide as the
// roll and returns the receipt.
func thermalTemplate[M printmodel.Model](draw func(*canvas, M, Options) error) func(M, Options) ([]byte, error) {
	return func(m M, o Options) ([]byte, error) {
		c, err := newCanvas(dotsWide(o.Paper))
		if err != nil {
			return nil, err
		}
		defer c.close()

		if err := draw(c, m, o); err != nil {
			return nil, err
		}

		c.feed(48)

		return c.receipt(), nil
	}
}

// thermalLetterhead draws the garage's name and legal details, centered.
func thermalLetterhead(c *canvas, g printmodel.GarageHeader, o Options, copyMark string) error {
	if o.Copy {
		if err := c.box(copyMark); err != nil {
			return err
		}
	}

	if err := c.text(g.Name, sizeLarge, true, alignCenter); err != nil {
		return err
	}

	for _, line := range []string{g.Address, labelled("MST", g.TaxCode), labelled("ĐT", g.Phone)} {
		if line == "" {
			continue
		}

		if err := c.text(line, sizeSmall, false, alignCenter); err != nil {
			return err
		}
	}

	c.rule()

	return nil
}
