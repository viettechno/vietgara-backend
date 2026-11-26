package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	"google.golang.org/protobuf/proto"
)

// ---- Printers, roll prints and labels (Release 13.0) -------------------------

func printerPath(garageID, printerID string) string {
	return garagePath(garageID) + "/printers/" + url.PathEscape(printerID)
}

// ListPrinters lists the garage's network printers.
func (c *Client) ListPrinters(ctx context.Context, garageID string) ([]*documentv1.Printer, error) {
	resp, err := call[documentv1.ListPrintersResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/printers", nil)

	return resp.GetPrinters(), err
}

// CreatePrinter saves a printer.
func (c *Client) CreatePrinter(ctx context.Context, garageID string, p *documentv1.Printer) (*documentv1.Printer, error) {
	return call[documentv1.Printer](ctx, c, http.MethodPost, garagePath(garageID)+"/printers", p)
}

// UpdatePrinter replaces a printer.
func (c *Client) UpdatePrinter(ctx context.Context, garageID, printerID string, p *documentv1.Printer) (*documentv1.Printer, error) {
	return call[documentv1.Printer](ctx, c, http.MethodPut, printerPath(garageID, printerID), p)
}

// DeletePrinter removes a printer.
func (c *Client) DeletePrinter(ctx context.Context, garageID, printerID string) error {
	_, err := call[documentv1.DeletePrinterResponse](ctx, c, http.MethodDelete, printerPath(garageID, printerID), nil)

	return err
}

// PrintPrinterTestPage returns a printer's test page and its content type.
func (c *Client) PrintPrinterTestPage(ctx context.Context, garageID, printerID string) (string, []byte, error) {
	return c.downloadWith(ctx, http.MethodPost, printerPath(garageID, printerID)+"/test-page:print", nil)
}

// PrintQuoteEscPos prints a quote on a K80 or K57 roll, as ESC/POS.
func (c *Client) PrintQuoteEscPos(ctx context.Context, garageID, quoteID string, paper documentv1.PaperSize) (string, []byte, error) {
	return c.downloadWith(ctx, http.MethodPost, garagePath(garageID)+"/quotes/"+url.PathEscape(quoteID)+"/escpos:print",
		&documentv1.PrintQuoteEscPosRequest{PaperSize: paper})
}

// PrintPartLabels prints the labels of parts, as ESC/POS.
func (c *Client) PrintPartLabels(
	ctx context.Context, garageID string, paper documentv1.PaperSize, parts []*documentv1.PartLabelCopies,
) (string, []byte, error) {
	return c.downloadWith(ctx, http.MethodPost, garagePath(garageID)+"/part-labels:print",
		&documentv1.PrintPartLabelsRequest{PaperSize: paper, Parts: parts})
}

// downloadWith sends in as the JSON body and answers the raw response with
// its content type, like DownloadRaw.
func (c *Client) downloadWith(ctx context.Context, method, path string, in proto.Message) (string, []byte, error) {
	req, err := c.newRequest(ctx, method, path, in)
	if err != nil {
		return "", nil, err
	}

	req.Header.Set("Accept", "*/*")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("%s %s: read body: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, decodeError(resp.StatusCode, raw)
	}

	return resp.Header.Get("Content-Type"), raw, nil
}
