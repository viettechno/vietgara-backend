package jobs_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

var (
	escInit = []byte{0x1b, 0x40}             // ESC @
	escCut  = []byte{0x1d, 0x56, 0x42, 0x00} // GS V B 0
	gsFF    = []byte{0x1d, 0x0c}             // next label
)

// Scenario: E2E-DOC-006, Release 13.0, a garage saves its network printers:
// defaults filled in, names unique, a document a printer cannot take
// refused; a member without garage.write lists them and prints a test page
// but cannot change them, another garage sees none (FR-DOC-07).
func TestGaragePrinters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-006")
	api, garageID := k.owner.API, k.garageID

	counter, err := api.CreatePrinter(ctx, garageID, &documentv1.Printer{
		Name: "Quầy thu ngân", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_RAW, Host: "192.168.1.50",
		PaperSize:     documentv1.PaperSize_PAPER_SIZE_K80,
		DocumentTypes: []documentv1.DocumentType{documentv1.DocumentType_DOCUMENT_TYPE_PAYMENT_VOUCHER},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(9100), counter.GetPort())

	office, err := api.CreatePrinter(ctx, garageID, &documentv1.Printer{
		Name: "Văn phòng", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_IPP, Host: "EPSON-L3250.local.",
		DocumentTypes: []documentv1.DocumentType{documentv1.DocumentType_DOCUMENT_TYPE_REPAIR_ORDER},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(631), office.GetPort())
	assert.Equal(t, "ipp/print", office.GetPath())

	_, err = api.CreatePrinter(ctx, garageID, &documentv1.Printer{
		Name: "QUẦY THU NGÂN", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_RAW, Host: "192.168.1.51",
		PaperSize: documentv1.PaperSize_PAPER_SIZE_K57,
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	_, err = api.CreatePrinter(ctx, garageID, &documentv1.Printer{
		Name: "Tem", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_RAW, Host: "192.168.1.52",
		PaperSize:     documentv1.PaperSize_PAPER_SIZE_K80,
		DocumentTypes: []documentv1.DocumentType{documentv1.DocumentType_DOCUMENT_TYPE_REPAIR_ORDER},
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	_, err = api.CreatePrinter(ctx, garageID, &documentv1.Printer{
		Name: "Quầy A5", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_IPP, Host: "192.168.1.53",
		PaperSize:     documentv1.PaperSize_PAPER_SIZE_A5,
		DocumentTypes: []documentv1.DocumentType{documentv1.DocumentType_DOCUMENT_TYPE_STOCK_ISSUE},
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	counter.PaperSize = documentv1.PaperSize_PAPER_SIZE_K57
	updated, err := api.UpdatePrinter(ctx, garageID, counter.GetId(), counter)
	require.NoError(t, err)
	assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_K57, updated.GetPaperSize())

	member := staffIn(t, k, "print-006-member", "Inventory Staff")

	seen, err := member.API.ListPrinters(ctx, garageID)
	require.NoError(t, err)
	require.Len(t, seen, 2)
	assert.Equal(t, "Quầy thu ngân", seen[0].GetName())

	contentType, page, err := member.API.PrintPrinterTestPage(ctx, garageID, counter.GetId())
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", contentType)
	assert.True(t, bytes.HasPrefix(page, escInit))

	contentType, page, err = member.API.PrintPrinterTestPage(ctx, garageID, office.GetId())
	require.NoError(t, err)
	assert.Contains(t, contentType, "pdf")
	assert.Contains(t, string(page), a4Box)

	err = member.API.DeletePrinter(ctx, garageID, counter.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	stranger := fixture.NewVerifiedOwner(t, "print-006-stranger")
	_, err = stranger.API.ListPrinters(ctx, garageID)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	require.NoError(t, api.DeletePrinter(ctx, garageID, counter.GetId()))
	err = api.DeletePrinter(ctx, garageID, counter.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	left, err := api.ListPrinters(ctx, garageID)
	require.NoError(t, err)
	assert.Len(t, left, 1)
}

// Scenario: E2E-DOC-007, Release 13.0, a quote prints on a K80 or K57 roll as
// ESC/POS; a reprint carries the copy mark when the garage turned it on; a
// page size is refused for a roll print (FR-DOC-06).
func TestRollPrint(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-007")
	api, garageID := k.owner.API, k.garageID
	quote := draft(t, k, quoteLabor("Thay dầu động cơ", 250_000))

	contentType, first, err := api.PrintQuoteEscPos(ctx, garageID, quote.GetId(), documentv1.PaperSize_PAPER_SIZE_K80)
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", contentType)
	assert.True(t, bytes.HasPrefix(first, escInit))
	assert.True(t, bytes.HasSuffix(first, escCut))
	assert.Equal(t, []byte{0x1d, 0x76, 0x30, 0x00, 72, 0}, first[2:8], "raster rows of 72 bytes: 576 dots on K80")

	_, narrow, err := api.PrintQuoteEscPos(ctx, garageID, quote.GetId(), documentv1.PaperSize_PAPER_SIZE_K57)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x1d, 0x76, 0x30, 0x00, 48, 0}, narrow[2:8], "raster rows of 48 bytes: 384 dots on K57")

	_, again, err := api.PrintQuoteEscPos(ctx, garageID, quote.GetId(), documentv1.PaperSize_PAPER_SIZE_K80)
	require.NoError(t, err)
	assert.Equal(t, first, again, "the mark is off: a reprint is the first print")

	_, err = api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_A4, true)
	require.NoError(t, err)

	_, marked, err := api.PrintQuoteEscPos(ctx, garageID, quote.GetId(), documentv1.PaperSize_PAPER_SIZE_K80)
	require.NoError(t, err)
	assert.NotEqual(t, first, marked, "the mark is on: a reprint carries it")

	_, _, err = api.PrintQuoteEscPos(ctx, garageID, quote.GetId(), documentv1.PaperSize_PAPER_SIZE_A4)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	_, err = api.UpdatePrintSetting(ctx, garageID, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.PaperSize_PAPER_SIZE_K80, false)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
}

// Scenario: E2E-DOC-008, Release 13.0, barcode labels of parts print as
// ESC/POS on 50 x 30 mm labels, one per copy; the limits are refused and a
// part of another garage is not found (FR-INV-13).
func TestPartLabels(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-008")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)

	_, err := api.SetPartBarcodes(ctx, garageID, c.filter.GetId(), []*cataloguev1.PartBarcode{{Barcode: "8934567890123"}})
	require.NoError(t, err)

	contentType, labels, err := api.PrintPartLabels(ctx, garageID, documentv1.PaperSize_PAPER_SIZE_LABEL_50X30, []*documentv1.PartLabelCopies{
		{PartId: c.filter.GetId(), Copies: 3}, {PartId: c.oil.GetId(), Copies: 1},
	})
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", contentType)
	assert.True(t, bytes.HasPrefix(labels, escInit))
	assert.Equal(t, 4, bytes.Count(labels, gsFF), "one label per copy")

	for _, parts := range [][]*documentv1.PartLabelCopies{
		{{PartId: c.filter.GetId(), Copies: 0}},
		{{PartId: c.filter.GetId(), Copies: 101}},
		{{PartId: c.filter.GetId(), Copies: 1}, {PartId: c.filter.GetId(), Copies: 1}},
		nil,
	} {
		_, _, err = api.PrintPartLabels(ctx, garageID, documentv1.PaperSize_PAPER_SIZE_LABEL_50X30, parts)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	}

	_, _, err = api.PrintPartLabels(ctx, garageID, documentv1.PaperSize_PAPER_SIZE_K80, []*documentv1.PartLabelCopies{{PartId: c.filter.GetId(), Copies: 1}})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	other := setup(t, "print-008-other")
	_, _, err = other.owner.API.PrintPartLabels(ctx, other.garageID, documentv1.PaperSize_PAPER_SIZE_LABEL_50X30,
		[]*documentv1.PartLabelCopies{{PartId: c.filter.GetId(), Copies: 1}})
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")
}
