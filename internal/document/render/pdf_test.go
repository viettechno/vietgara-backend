package render_test

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/render"
)

// a4 prints m with the newest A4 template of its document type.
func a4(m printmodel.Model) ([]byte, error) {
	tpl, ok := render.Latest(m.Type(), printmodel.A4)
	if !ok {
		return nil, render.ErrModelMismatch
	}

	return tpl.Render(m, false)
}

func logoPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 40, 20))

	for x := range 40 {
		for y := range 20 {
			img.Set(x, y, color.RGBA{R: 20, G: 120, B: 200, A: 255})
		}
	}

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

func logoJPEG(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 30, 30)), nil))

	return buf.Bytes()
}

func quote(header printmodel.GarageHeader) printmodel.Quote {
	return printmodel.Quote{
		Garage: header, Number: "BG-1", Status: "draft", CustomerName: "An", VehiclePlate: "51F-123.45",
		Lines: []printmodel.DocumentLine{{Description: "Oil", Quantity: "1", UnitPrice: 100, LineTotal: 100}}, TotalAmount: 100,
	}
}

// TestLetterhead covers FR-TEN-06: the legal identity and the logo in the
// header of a document, and a document that still renders when the logo
// cannot be drawn.
func TestLetterhead(t *testing.T) {
	t.Parallel()

	full := printmodel.GarageHeader{
		Name: "Cong ty TNHH Gara An Phat", Address: "12 Nguyen Hue, Q1", Phone: "0281234567", TaxCode: "0301234567",
		Representative: "Nguyen Van An", Bank: "Vietcombank - 0011 2233 - An Phat",
	}

	withoutLogo, err := a4(quote(full))
	require.NoError(t, err)
	require.Equal(t, "%PDF", string(withoutLogo[:4]))

	for name, logo := range map[string][]byte{
		"png":          logoPNG(t),
		"jpeg":         logoJPEG(t),
		"webp":         []byte("RIFF\x00\x00\x00\x00WEBPVP8 "),
		"not an image": []byte("hello"),
		"corrupt png":  append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...),
	} {
		header := full
		header.Logo = logo

		pdf, err := a4(quote(header))
		require.NoError(t, err, name)
		assert.Equal(t, "%PDF", string(pdf[:4]), name)
	}

	header := full
	header.Logo = logoPNG(t)

	withLogo, err := a4(quote(header))
	require.NoError(t, err)
	assert.Greater(t, len(withLogo), len(withoutLogo), "the logo is embedded")

	minimal, err := a4(quote(printmodel.GarageHeader{Name: "Gara"}))
	require.NoError(t, err)
	assert.Less(t, len(minimal), len(withoutLogo), "a header without a legal profile is shorter")
}

// TestBillTo covers FR-CUS-07: a document billed to a tax profile prints it,
// and a retail receipt prints nothing extra.
func TestBillTo(t *testing.T) {
	t.Parallel()

	billed := quote(printmodel.GarageHeader{Name: "Gara"})
	billed.BillTo = printmodel.BillTo{Name: "Cong ty Van Tai A", TaxID: "0301234567", Address: "12 Le Loi"}

	withParty, err := a4(billed)
	require.NoError(t, err)

	retail, err := a4(quote(printmodel.GarageHeader{Name: "Gara"}))
	require.NoError(t, err)

	assert.NotEqual(t, len(withParty), len(retail), "the billed document carries the party's lines")
	assert.True(t, printmodel.BillTo{}.IsZero())

	for name, renderFn := range map[string]func() ([]byte, error){
		"repair order": func() ([]byte, error) {
			return a4(printmodel.RepairOrder{BillTo: billed.BillTo, Number: "LSC-1"})
		},
		"receipt": func() ([]byte, error) {
			return a4(printmodel.Settlement{BillTo: billed.BillTo, Number: "QT-1"})
		},
	} {
		pdf, err := renderFn()
		require.NoError(t, err, name)
		assert.Equal(t, "%PDF", string(pdf[:4]), name)
	}
}
