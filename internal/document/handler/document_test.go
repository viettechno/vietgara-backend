package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"

	"github.com/viettechno/vietgara-backend/internal/document/handler"
	"github.com/viettechno/vietgara-backend/internal/document/render"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	garageID      = "11111111-1111-1111-1111-111111111111"
	quoteID       = "22222222-2222-2222-2222-222222222222"
	settlementID  = "33333333-3333-3333-3333-333333333333"
	itemID        = "44444444-4444-4444-4444-444444444444"
	repairOrderID = "55555555-5555-5555-5555-555555555555"
)

var anyArg = mock.Anything

type env struct {
	srv            *handler.Server
	quotes         *mocks.MockDocumentQuoteReader
	repairOrders   *mocks.MockDocumentRepairOrderReader
	settlements    *mocks.MockDocumentSettlementReader
	stockMovements *mocks.MockDocumentStockMovementReader
	garages        *mocks.MockDocumentGarageReader
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		quotes: mocks.NewMockDocumentQuoteReader(t), repairOrders: mocks.NewMockDocumentRepairOrderReader(t),
		settlements: mocks.NewMockDocumentSettlementReader(t), stockMovements: mocks.NewMockDocumentStockMovementReader(t),
		garages: mocks.NewMockDocumentGarageReader(t),
	}

	uc := usecase.NewDocumentUseCase(e.quotes, e.repairOrders, e.settlements, e.stockMovements, e.garages)
	e.srv = handler.NewServer(uc)

	return e
}

func defaultGarage() usecase.GarageHeaderInfo {
	return usecase.GarageHeaderInfo{Name: "District 1 Garage", Address: "123 Le Loi", Phone: "028-1234-5678"}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 4)
	assert.NotNil(t, p[documentv1.DocumentService_ExportQuotePdf_FullMethodName].Garage)
}

func TestExportQuotePdf(t *testing.T) {
	t.Parallel()

	t.Run("returns a PDF HttpBody", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()
		e.quotes.EXPECT().Get(anyArg, garageID, quoteID).Return(usecase.QuoteInfo{
			Number: "QT-0001", CustomerName: "An Nguyen", TotalAmount: 250_000,
			Lines: []render.DocumentLine{{Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000}},
		}, nil).Once()

		resp, err := e.srv.ExportQuotePdf(t.Context(), &documentv1.ExportQuotePdfRequest{GarageId: garageID, QuoteId: quoteID})
		require.NoError(t, err)
		assert.Equal(t, "application/pdf", resp.GetContentType())
		assert.NotEmpty(t, resp.GetData())
	})

	t.Run("quote not found", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()

		notFound := apperror.Wrap(apperror.CodeNotFound, "quote not found", errDocumentTestBoom)
		e.quotes.EXPECT().Get(anyArg, garageID, quoteID).Return(usecase.QuoteInfo{}, notFound).Once()

		_, err := e.srv.ExportQuotePdf(t.Context(), &documentv1.ExportQuotePdfRequest{GarageId: garageID, QuoteId: quoteID})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestExportRepairOrderPdf(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()
	e.repairOrders.EXPECT().Get(anyArg, garageID, repairOrderID).Return(usecase.RepairOrderInfo{
		Number: "LSC-0001", CustomerName: "An Nguyen", TotalAmount: 250_000,
		Lines: []render.DocumentLine{{Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000}},
	}, nil).Once()

	resp, err := e.srv.ExportRepairOrderPdf(t.Context(), &documentv1.ExportRepairOrderPdfRequest{
		GarageId: garageID, RepairOrderId: repairOrderID,
	})
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", resp.GetContentType())
	assert.NotEmpty(t, resp.GetData())
}

func TestExportSettlementReceiptPdf(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()
	e.settlements.EXPECT().Get(anyArg, garageID, settlementID).Return(usecase.SettlementInfo{
		Number: "QT-0001", CustomerName: "An Nguyen", TotalAmount: 1_000_000,
	}, nil).Once()

	resp, err := e.srv.ExportSettlementReceiptPdf(t.Context(), &documentv1.ExportSettlementReceiptPdfRequest{
		GarageId: garageID, SettlementId: settlementID,
	})
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", resp.GetContentType())
	assert.NotEmpty(t, resp.GetData())
}

func TestExportStockMovementsExcel(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.stockMovements.EXPECT().List(anyArg, garageID, itemID).Return([]render.StockMovementRow{
		{Date: "2026-09-30", SKU: "OIL-5W30", ItemName: "Engine oil 5W30", Direction: "in", Quantity: "10", Reference: "SR-1"},
	}, nil).Once()

	resp, err := e.srv.ExportStockMovementsExcel(t.Context(), &documentv1.ExportStockMovementsExcelRequest{
		GarageId: garageID, ItemId: itemID,
	})
	require.NoError(t, err)
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", resp.GetContentType())
	assert.NotEmpty(t, resp.GetData())
}
