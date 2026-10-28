package jobs_test

import (
	"bytes"
	"compress/zlib"
	"io"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

const (
	a4Box = "/MediaBox [0 0 595.28 841.89]"
	a5Box = "/MediaBox [0 0 420.94 595.28]"
)

var pdfStream = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)

// pageContent is what a PDF draws on its pages: the decompressed content
// streams, which begin with the line cap and join settings. It leaves out the
// creation date and the embedded fonts, so two prints of one document compare
// equal.
func pageContent(t *testing.T, pdf []byte) string {
	t.Helper()

	var content bytes.Buffer

	for _, match := range pdfStream.FindAllSubmatch(pdf, -1) {
		reader, err := zlib.NewReader(bytes.NewReader(match[1]))
		if err != nil {
			continue
		}

		data, err := io.ReadAll(reader)
		require.NoError(t, err)

		if bytes.HasPrefix(data, []byte("0 J\n0 j\n")) {
			content.Write(data)
		}
	}

	require.NotZero(t, content.Len(), "the PDF has page content")

	return content.String()
}

// Scenario: E2E-DOC-001, Release 10.0, a print answers the PDF; an export
// follows the same layout (FR-DOC-04, FR-DOC-05). That a print is logged and
// an export is not is checked by the backend's integration tests: the log has
// no API.
func TestPrintAnswersThePDF(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-001")
	api, garageID := k.owner.API, k.garageID

	got, err := api.GetPrintSettings(ctx, garageID)
	require.NoError(t, err)
	require.Len(t, got.GetSettings(), 6)

	for _, setting := range got.GetSettings() {
		assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_A4, setting.GetPaperSize(), setting.GetDocumentType().String())
		assert.False(t, setting.GetCopyMark())
	}

	assert.Equal(t, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, got.GetSettings()[0].GetDocumentType())
	assert.Equal(t, []documentv1.PaperSize{documentv1.PaperSize_PAPER_SIZE_A4, documentv1.PaperSize_PAPER_SIZE_A5}, got.GetSettings()[0].GetAvailablePaperSizes())
	assert.Equal(t, []documentv1.PaperSize{documentv1.PaperSize_PAPER_SIZE_A4}, got.GetSettings()[1].GetAvailablePaperSizes(), "a repair order is A4 only")

	quote := draft(t, k, quoteLabor("Work", 100_000))

	contentType, printed, err := api.PrintQuotePDF(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.Contains(t, contentType, "pdf")
	assert.Contains(t, string(printed), a4Box)

	_, exported, err := api.DownloadQuotePDF(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, pageContent(t, printed), pageContent(t, exported), "an export has the layout of the print")
}

// Scenario: E2E-DOC-002, Release 10.0, A5 for a document: a print and an
// export follow it; a size without a template is refused (FR-DOC-04).
func TestChooseA5(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-002")
	api, garageID := k.owner.API, k.garageID
	quote := draft(t, k, quoteLabor("Work", 100_000))

	chosen, err := api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_A5, false)
	require.NoError(t, err)
	assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_A5, chosen.GetPaperSize())

	_, printed, err := api.PrintQuotePDF(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.Contains(t, string(printed), a5Box)

	_, exported, err := api.DownloadQuotePDF(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.Contains(t, string(exported), a5Box, "an export follows the garage's size")

	_, err = api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_REPAIR_ORDER, documentv1.PaperSize_PAPER_SIZE_A5, false)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	_, err = api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_UNSPECIFIED, false)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	got, err := api.GetPrintSettings(ctx, garageID)
	require.NoError(t, err)
	assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_A4, got.GetSettings()[1].GetPaperSize(), "the refused setting changed nothing")
}

// Scenario: E2E-DOC-003, Release 10.0, a reprint carries the copy mark only
// when the garage turned it on; the first print and an export never do
// (FR-DOC-05).
func TestCopyMarkOnReprint(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-003")
	api, garageID := k.owner.API, k.garageID
	quote := draft(t, k, quoteLabor("Work", 100_000))

	_, first, err := api.PrintQuotePDF(ctx, garageID, quote.GetId())
	require.NoError(t, err)

	_, second, err := api.PrintQuotePDF(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, pageContent(t, first), pageContent(t, second), "the mark is off: a reprint looks like the first print")

	_, err = api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_A4, true)
	require.NoError(t, err)

	_, third, err := api.PrintQuotePDF(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.NotEqual(t, pageContent(t, first), pageContent(t, third), "the mark is on: a reprint carries it")

	_, exported, err := api.DownloadQuotePDF(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, pageContent(t, first), pageContent(t, exported), "an export never carries the mark")

	fresh := draft(t, k, quoteLabor("Other work", 50_000))

	_, firstOfFresh, err := api.PrintQuotePDF(ctx, garageID, fresh.GetId())
	require.NoError(t, err)

	_, reprintOfFresh, err := api.PrintQuotePDF(ctx, garageID, fresh.GetId())
	require.NoError(t, err)
	assert.NotEqual(t, pageContent(t, firstOfFresh), pageContent(t, reprintOfFresh), "the first print of a document never carries the mark; its reprint does")
}

// Scenario: E2E-DOC-004, Release 10.0, the print settings belong to the
// garage: a member without garage.write reads but cannot change them, and
// another garage's owner can do neither (FR-DOC-04).
func TestPrintSettingsBelongToTheGarage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-004")
	api, garageID := k.owner.API, k.garageID

	member := staffIn(t, k, "print-member", "Inventory Staff")

	seen, err := member.API.GetPrintSettings(ctx, garageID)
	require.NoError(t, err)
	assert.Len(t, seen.GetSettings(), 6)

	_, err = member.API.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_A5, false)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	stranger := fixture.NewVerifiedOwner(t, "print-stranger")

	_, err = stranger.API.GetPrintSettings(ctx, garageID)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	_, err = stranger.API.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_A5, false)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	_, err = api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_A5, false)
	require.NoError(t, err)

	mine, err := member.API.GetPrintSettings(ctx, garageID)
	require.NoError(t, err)
	assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_A5, mine.GetSettings()[0].GetPaperSize(), "the owner's change is what the member sees")
}
