package jobs_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// TestPrintSettingsAndPrinting covers FR-DOC-04 and FR-DOC-05 (Release 10.0):
// the garage's default paper size per document type, printing on A5, and the
// rule that a garage sees and changes only its own settings.
func TestPrintSettingsAndPrinting(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print")
	api, garageID := k.owner.API, k.garageID

	t.Run("a garage that never chose prints A4 and offers A5 for a quote", func(t *testing.T) {
		got, err := api.GetPrintSettings(ctx, garageID)
		require.NoError(t, err)
		require.Len(t, got.GetSettings(), 6)

		for _, setting := range got.GetSettings() {
			assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_A4, setting.GetPaperSize(), setting.GetDocumentType().String())
			assert.False(t, setting.GetCopyMark())
		}

		quote := got.GetSettings()[0]
		assert.Equal(t, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, quote.GetDocumentType())
		assert.Equal(t, []documentv1.PaperSize{documentv1.PaperSize_PAPER_SIZE_A4, documentv1.PaperSize_PAPER_SIZE_A5}, quote.GetAvailablePaperSizes())
	})

	t.Run("a document prints on the chosen paper, an export follows the same choice", func(t *testing.T) {
		quote := draft(t, k, quoteLabor("Work", 100_000))

		_, a4, err := api.PrintQuotePDF(ctx, garageID, quote.GetId())
		require.NoError(t, err)
		assert.Contains(t, string(a4), "/MediaBox [0 0 595.28 841.89]")

		chosen, err := api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_A5, true)
		require.NoError(t, err)
		assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_A5, chosen.GetPaperSize())
		assert.True(t, chosen.GetCopyMark())

		contentType, a5, err := api.PrintQuotePDF(ctx, garageID, quote.GetId())
		require.NoError(t, err)
		assert.Contains(t, contentType, "pdf")
		assert.Contains(t, string(a5), "/MediaBox [0 0 420.94 595.28]")

		_, exported, err := api.DownloadQuotePDF(ctx, garageID, quote.GetId())
		require.NoError(t, err)
		assert.Contains(t, string(exported), "/MediaBox [0 0 420.94 595.28]", "an export follows the garage's size")
	})

	t.Run("a size the document has no template for is refused", func(t *testing.T) {
		_, err := api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_REPAIR_ORDER, documentv1.PaperSize_PAPER_SIZE_A5, false)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

		_, err = api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_UNSPECIFIED, false)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	})

	t.Run("another garage's owner cannot read or change the settings", func(t *testing.T) {
		stranger := fixture.NewVerifiedOwner(t, "print-stranger")

		_, err := stranger.API.GetPrintSettings(ctx, garageID)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

		_, err = stranger.API.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_A4, false)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

		mine, err := api.GetPrintSettings(ctx, garageID)
		require.NoError(t, err)
		assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_A5, mine.GetSettings()[0].GetPaperSize(), "unchanged")
	})
}
