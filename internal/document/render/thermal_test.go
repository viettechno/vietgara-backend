package render_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/printmodel/fixture"
	"github.com/viettechno/vietgara-backend/internal/document/render"
)

var (
	escInit = []byte{0x1b, 0x40}
	escCut  = []byte{0x1d, 0x56, 0x42, 0x00}
	gsFF    = []byte{0x1d, 0x0c}
)

func TestRollTemplates(t *testing.T) {
	t.Parallel()

	for _, typ := range []printmodel.DocumentType{printmodel.TypeQuote, printmodel.TypePaymentVoucher} {
		for _, paper := range []printmodel.Paper{printmodel.K80, printmodel.K57} {
			tpl, ok := render.Latest(typ, paper)
			require.True(t, ok, "%s %s", typ, paper)
			assert.Equal(t, 1, tpl.Version)
		}
	}

	_, ok := render.Latest(printmodel.TypeRepairOrder, printmodel.K80)
	assert.False(t, ok, "a repair order has no roll template")
	assert.Equal(t, []printmodel.Paper{printmodel.A4, printmodel.A5}, render.Papers(printmodel.TypePaymentVoucher), "the rolls are not a print setting")
}

// TestRollOutput checks the ESC/POS of every roll template: reset, raster
// bands as wide as the roll, the cut, and the copy mark changing the output.
func TestRollOutput(t *testing.T) {
	t.Parallel()

	widths := map[printmodel.Paper]byte{printmodel.K80: 72, printmodel.K57: 48}

	for _, m := range fixture.All() {
		for paper, rowBytes := range widths {
			tpl, ok := render.Latest(m.Type(), paper)
			if !ok {
				continue
			}

			plain, err := tpl.Render(m, false)
			require.NoError(t, err)
			assert.True(t, bytes.HasPrefix(plain, escInit), "%s %s", m.Type(), paper)
			assert.True(t, bytes.HasSuffix(plain, escCut))
			assert.Equal(t, []byte{0x1d, 0x76, 0x30, 0x00, rowBytes, 0}, plain[2:8])
			assert.LessOrEqual(t, int(plain[8])|int(plain[9])<<8, 128, "bands of at most 128 rows")

			copied, err := tpl.Render(m, true)
			require.NoError(t, err)
			assert.NotEqual(t, plain, copied)
		}
	}
}

// TestReleasedRollTemplatesAreImmutable is TestReleasedTemplatesAreImmutable
// for the rolls: the ESC/POS output is deterministic, so its hash is the
// fingerprint. A change of the font rasterizer (golang.org/x/image) shows
// here too: check the output on paper before regenerating.
func TestReleasedRollTemplatesAreImmutable(t *testing.T) {
	t.Parallel()

	got := map[string]string{}

	for _, m := range fixture.All() {
		for _, paper := range []printmodel.Paper{printmodel.K80, printmodel.K57} {
			for version := 1; ; version++ {
				tpl, ok := render.Lookup(m.Type(), paper, version)
				if !ok {
					break
				}

				for _, copyMark := range []bool{false, true} {
					data, err := tpl.Render(m, copyMark)
					require.NoError(t, err)

					sum := sha256.Sum256(data)
					got[fmt.Sprintf("%s/%s/v%d/copy=%t", m.Type(), paper, version, copyMark)] = hex.EncodeToString(sum[:])
				}
			}
		}
	}

	const file = "testdata/roll_fingerprints.json"

	if os.Getenv("UPDATE_GOLDEN") != "" {
		data, err := json.MarshalIndent(got, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(file, append(data, '\n'), 0o600))
	}

	raw, err := os.ReadFile(file)
	require.NoError(t, err)

	var want map[string]string
	require.NoError(t, json.Unmarshal(raw, &want))
	assert.Equal(t, want, got)
}

func TestPartLabels(t *testing.T) {
	t.Parallel()

	labels := []printmodel.PartLabel{
		{Name: "Lọc dầu động cơ Toyota Vios 2018-2022 chính hãng loại tốt nhất", SKU: "LD-001", Barcode: "8934567890123"},
		{Name: "Lọc dầu động cơ Toyota Vios 2018-2022 chính hãng loại tốt nhất", SKU: "LD-001", Barcode: "8934567890123"},
		{Name: "Dây", SKU: strings.Repeat("SKU-RẤT-DÀI-", 6), Barcode: "D-1"},
	}

	data, err := render.PartLabels(printmodel.Label5030, labels)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(data, escInit))
	assert.Equal(t, 3, bytes.Count(data, gsFF))
	// GS v 0: 50 bytes (400 dots) a row, 128 rows then 112: one 240-dot label.
	assert.Equal(t, []byte{0x1d, 0x76, 0x30, 0x00, 50, 0, 128, 0}, data[2:10])

	for _, code := range []string{"Mã có dấu", strings.Repeat("X", 30)} {
		_, err = render.PartLabels(printmodel.Label5030, []printmodel.PartLabel{{Name: "N", SKU: "S", Barcode: code}})
		require.ErrorIs(t, err, render.ErrBarcodeUnreadable, code)
	}

	_, err = render.PartLabels(printmodel.K80, labels)
	require.ErrorIs(t, err, render.ErrNotLabelPaper)
}

func TestTestPages(t *testing.T) {
	t.Parallel()

	for _, paper := range []printmodel.Paper{printmodel.K80, printmodel.K57, printmodel.Label5030} {
		data, err := render.TestPageEscPos(paper, "Gara An Phát", "Quầy")
		require.NoError(t, err, paper)
		assert.True(t, bytes.HasPrefix(data, escInit), paper)
	}

	pdf, err := render.TestPagePDF("Gara An Phát", "Văn phòng")
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(pdf, []byte("%PDF")))
}
