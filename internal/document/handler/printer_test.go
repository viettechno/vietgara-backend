package handler_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"

	"github.com/viettechno/vietgara-backend/internal/document/handler"
	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// memPrinters is an in-memory usecase.PrinterStore.
type memPrinters struct{ printers []usecase.Printer }

func (m *memPrinters) ListPrinters(context.Context, string) ([]usecase.Printer, error) {
	return m.printers, nil
}

func (m *memPrinters) GetPrinter(_ context.Context, _, id string) (usecase.Printer, error) {
	for _, p := range m.printers {
		if p.ID == id {
			return p, nil
		}
	}

	return usecase.Printer{}, usecase.ErrPrinterNotFound
}

func (m *memPrinters) LockPrinters(context.Context, string) error { return nil }

func (m *memPrinters) CountPrinters(context.Context, string) (int, error) {
	return len(m.printers), nil
}

func (m *memPrinters) CreatePrinter(_ context.Context, _ string, p usecase.Printer) (usecase.Printer, error) {
	p.ID = "p-1"
	m.printers = append(m.printers, p)

	return p, nil
}

func (m *memPrinters) UpdatePrinter(_ context.Context, _ string, p usecase.Printer) (usecase.Printer, error) {
	if _, err := m.GetPrinter(context.Background(), "", p.ID); err != nil {
		return usecase.Printer{}, err
	}

	m.printers = []usecase.Printer{p}

	return p, nil
}

func (m *memPrinters) DeletePrinter(_ context.Context, _, id string) error {
	if _, err := m.GetPrinter(context.Background(), "", id); err != nil {
		return err
	}

	m.printers = nil

	return nil
}

type stubParts struct{}

func (stubParts) PartLabel(_ context.Context, _, partID string) (printmodel.PartLabel, error) {
	return printmodel.PartLabel{Name: "Part " + partID, SKU: partID, Barcode: partID}, nil
}

func newPrintingServer(t *testing.T) (*handler.Server, *env) {
	t.Helper()

	e := newEnv(t)
	uc := usecase.NewDocumentUseCase(e.quotes, e.repairOrders, e.settlements, e.stockMovements, e.revenue, e.garages, e.inventory, e.prints).
		WithPrinting(&memPrinters{}, stubParts{})

	return handler.NewServer(uc), e
}

func TestPrinterPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Equal(t, access.GarageWrite, p[documentv1.DocumentService_CreatePrinter_FullMethodName].Garage.Permission)
	assert.Equal(t, access.GarageWrite, p[documentv1.DocumentService_DeletePrinter_FullMethodName].Garage.Permission)
	assert.Equal(t, access.QuoteRead, p[documentv1.DocumentService_PrintQuoteEscPos_FullMethodName].Garage.Permission)
	assert.Equal(t, access.BillingRead, p[documentv1.DocumentService_PrintPaymentVoucherEscPos_FullMethodName].Garage.Permission)
	assert.Equal(t, access.CatalogueRead, p[documentv1.DocumentService_PrintPartLabels_FullMethodName].Garage.Permission)
}

func TestPrinterRPCs(t *testing.T) {
	t.Parallel()

	srv, e := newPrintingServer(t)
	ctx := t.Context()

	created, err := srv.CreatePrinter(ctx, &documentv1.CreatePrinterRequest{GarageId: garageID, Printer: &documentv1.Printer{
		Name: "Tem", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_RAW, Host: "10.0.0.9",
		PaperSize:     documentv1.PaperSize_PAPER_SIZE_LABEL_50X30,
		DocumentTypes: []documentv1.DocumentType{documentv1.DocumentType_DOCUMENT_TYPE_PART_LABEL},
	}})
	require.NoError(t, err)
	assert.Equal(t, int32(9100), created.GetPort())
	assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_LABEL_50X30, created.GetPaperSize())
	assert.Equal(t, []documentv1.DocumentType{documentv1.DocumentType_DOCUMENT_TYPE_PART_LABEL}, created.GetDocumentTypes())

	updated, err := srv.UpdatePrinter(ctx, &documentv1.UpdatePrinterRequest{GarageId: garageID, PrinterId: created.GetId(), Printer: &documentv1.Printer{
		Name: "A4", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_IPP, Host: "10.0.0.9",
		DocumentTypes: []documentv1.DocumentType{documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, documentv1.DocumentType_DOCUMENT_TYPE_PAYMENT_OUT_VOUCHER},
	}})
	require.NoError(t, err)
	assert.Equal(t, documentv1.PrinterProtocol_PRINTER_PROTOCOL_IPP, updated.GetProtocol())
	assert.Equal(t, "ipp/print", updated.GetPath())
	assert.Equal(t, documentv1.PaperSize_PAPER_SIZE_UNSPECIFIED, updated.GetPaperSize())

	list, err := srv.ListPrinters(ctx, &documentv1.ListPrintersRequest{GarageId: garageID})
	require.NoError(t, err)
	require.Len(t, list.GetPrinters(), 1)

	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()

	page, err := srv.PrintPrinterTestPage(ctx, &documentv1.PrintPrinterTestPageRequest{GarageId: garageID, PrinterId: created.GetId()})
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", page.GetContentType())

	_, err = srv.DeletePrinter(ctx, &documentv1.DeletePrinterRequest{GarageId: garageID, PrinterId: created.GetId()})
	require.NoError(t, err)

	_, err = srv.DeletePrinter(ctx, &documentv1.DeletePrinterRequest{GarageId: garageID, PrinterId: created.GetId()})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = srv.PrintPrinterTestPage(ctx, &documentv1.PrintPrinterTestPageRequest{GarageId: garageID, PrinterId: "x"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = srv.CreatePrinter(ctx, &documentv1.CreatePrinterRequest{GarageId: garageID, Printer: &documentv1.Printer{
		Name: "X", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_IPP, Host: "10.0.0.9",
		DocumentTypes: []documentv1.DocumentType{documentv1.DocumentType_DOCUMENT_TYPE_UNSPECIFIED},
	}})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = srv.UpdatePrinter(ctx, &documentv1.UpdatePrinterRequest{GarageId: garageID, PrinterId: "x", Printer: &documentv1.Printer{
		DocumentTypes: []documentv1.DocumentType{99},
	}})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = srv.UpdatePrinter(ctx, &documentv1.UpdatePrinterRequest{GarageId: garageID, PrinterId: "x", Printer: &documentv1.Printer{
		Name: "X", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_IPP, Host: "10.0.0.9",
	}})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestPrintEscPosAndLabels(t *testing.T) {
	t.Parallel()

	srv, e := newPrintingServer(t)
	ctx := t.Context()

	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil)
	e.quotes.EXPECT().Get(anyArg, garageID, quoteID).Return(usecase.QuoteInfo{Number: "BG-1"}, nil)
	e.settlements.EXPECT().Get(anyArg, garageID, settlementID).Return(usecase.SettlementInfo{
		Vouchers: []usecase.VoucherInfo{{ID: "p", Number: "PT-1", Amount: 1000}},
	}, nil)

	body, err := srv.PrintQuoteEscPos(ctx, &documentv1.PrintQuoteEscPosRequest{GarageId: garageID, QuoteId: quoteID, PaperSize: documentv1.PaperSize_PAPER_SIZE_K80})
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", body.GetContentType())

	_, err = srv.PrintQuoteEscPos(ctx, &documentv1.PrintQuoteEscPosRequest{GarageId: garageID, QuoteId: quoteID})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = srv.PrintPaymentVoucherEscPos(ctx, &documentv1.PrintPaymentVoucherEscPosRequest{
		GarageId: garageID, SettlementId: settlementID, PaymentId: "p", PaperSize: documentv1.PaperSize_PAPER_SIZE_A4,
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	body, err = srv.PrintPaymentVoucherEscPos(ctx, &documentv1.PrintPaymentVoucherEscPosRequest{
		GarageId: garageID, SettlementId: settlementID, PaymentId: "p", PaperSize: documentv1.PaperSize_PAPER_SIZE_K57,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, body.GetData())

	labels, err := srv.PrintPartLabels(ctx, &documentv1.PrintPartLabelsRequest{
		GarageId: garageID, PaperSize: documentv1.PaperSize_PAPER_SIZE_LABEL_50X30,
		Parts: []*documentv1.PartLabelCopies{{PartId: "A-1", Copies: 2}},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, bytes.Count(labels.GetData(), []byte{0x1d, 0x0c}))

	_, err = srv.PrintPartLabels(ctx, &documentv1.PrintPartLabelsRequest{GarageId: garageID, PaperSize: documentv1.PaperSize_PAPER_SIZE_K80})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
