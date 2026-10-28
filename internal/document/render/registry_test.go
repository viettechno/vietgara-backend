package render_test

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/printmodel/fixture"
	"github.com/viettechno/vietgara-backend/internal/document/render"
)

// a5Documents are the documents that have an A5 template.
var a5Documents = []printmodel.DocumentType{printmodel.TypeQuote, printmodel.TypeSettlement, printmodel.TypePaymentVoucher}

func TestRegistry(t *testing.T) {
	t.Parallel()

	for _, typ := range printmodel.Types() {
		a4, ok := render.Latest(typ, printmodel.A4)
		require.True(t, ok, "every document has an A4 template: %s", typ)
		assert.Equal(t, 1, a4.Version)

		got, found := render.Lookup(typ, printmodel.A4, 1)
		require.True(t, found)
		assert.Equal(t, a4.Type, got.Type)

		_, found = render.Lookup(typ, printmodel.A4, 2)
		assert.False(t, found, "no second version yet")
	}

	assert.Equal(t, []printmodel.Paper{printmodel.A4, printmodel.A5}, render.Papers(printmodel.TypeQuote))
	assert.Equal(t, []printmodel.Paper{printmodel.A4}, render.Papers(printmodel.TypeRepairOrder))
	assert.Equal(t, []printmodel.Paper{printmodel.A4}, render.Papers(printmodel.TypeGoodsReceipt))
	assert.Equal(t, []printmodel.Paper{printmodel.A4}, render.Papers(printmodel.TypeStockIssue))
	assert.Empty(t, render.Papers(printmodel.DocumentType("invoice")))

	for _, typ := range a5Documents {
		_, ok := render.Latest(typ, printmodel.A5)
		assert.True(t, ok, typ)
	}

	_, ok := render.Latest(printmodel.TypeRepairOrder, printmodel.A5)
	assert.False(t, ok)
}

func TestTemplateRefusesAnotherDocumentsModel(t *testing.T) {
	t.Parallel()

	tpl, ok := render.Latest(printmodel.TypeQuote, printmodel.A4)
	require.True(t, ok)

	_, err := tpl.Render(printmodel.RepairOrder{}, false)
	require.ErrorIs(t, err, render.ErrModelMismatch)
}

// mediaBox is the page size, in PDF points.
var mediaBox = regexp.MustCompile(`/MediaBox \[0 0 ([0-9.]+) ([0-9.]+)\]`)

func TestPaperSizesAndCopyMark(t *testing.T) {
	t.Parallel()

	for _, m := range fixture.All() {
		for _, paper := range render.Papers(m.Type()) {
			tpl, ok := render.Latest(m.Type(), paper)
			require.True(t, ok)

			plain, err := tpl.Render(m, false)
			require.NoError(t, err, "%s %s", m.Type(), paper)

			match := mediaBox.FindSubmatch(plain)
			require.NotNil(t, match, "%s %s has a page size", m.Type(), paper)

			width := string(match[1])
			if paper == printmodel.A4 {
				assert.Equal(t, "595.28", width, "%s A4", m.Type())
			} else {
				assert.Equal(t, "420.94", width, "%s A5", m.Type())
			}

			copied, err := tpl.Render(m, true)
			require.NoError(t, err)
			assert.NotEqual(t, fingerprint(t, plain), fingerprint(t, copied), "%s %s carries the copy mark", m.Type(), paper)
		}
	}
}

var streamPattern = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)

// fingerprint hashes what a PDF draws: its page content streams. The
// embedded font programs and the creation date are left out, as they are not
// reproducible from one run to the next.
func fingerprint(t *testing.T, pdf []byte) string {
	t.Helper()

	hash := sha256.New()

	for _, match := range streamPattern.FindAllSubmatch(pdf, -1) {
		reader, err := zlib.NewReader(bytes.NewReader(match[1]))
		if err != nil {
			continue
		}

		data, err := io.ReadAll(reader)
		require.NoError(t, err)

		if isPageContent(data) {
			hash.Write(data)
		}
	}

	return hex.EncodeToString(hash.Sum(nil))
}

// isPageContent reports whether a decoded stream is a page's drawing
// operators; fpdf starts each with the line cap and join settings.
func isPageContent(data []byte) bool {
	return bytes.HasPrefix(data, []byte("0 J\n0 j\n"))
}

// TestReleasedTemplatesAreImmutable renders the fixture of every document
// with every registered template version and compares what it draws with the
// stored fingerprint: a released version that was edited fails here, and the
// fix is a new version, not a new fingerprint. Regenerate the file when a
// version is added, with UPDATE_GOLDEN=1.
func TestReleasedTemplatesAreImmutable(t *testing.T) {
	t.Parallel()

	got := map[string]string{}

	for _, m := range fixture.All() {
		for _, paper := range []printmodel.Paper{printmodel.A4, printmodel.A5} {
			for version := 1; ; version++ {
				tpl, ok := render.Lookup(m.Type(), paper, version)
				if !ok {
					break
				}

				for _, copyMark := range []bool{false, true} {
					pdf, err := tpl.Render(m, copyMark)
					require.NoError(t, err)

					got[fmt.Sprintf("%s/%s/v%d/copy=%t", m.Type(), paper, version, copyMark)] = fingerprint(t, pdf)
				}
			}
		}
	}

	const file = "testdata/fingerprints.json"

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
