package handler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"

	"github.com/viettechno/vietgara-backend/internal/document/handler"
	"github.com/viettechno/vietgara-backend/internal/document/render"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
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
	revenue        *mocks.MockDocumentRevenueReader
	garages        *mocks.MockDocumentGarageReader
	inventory      *mocks.MockDocumentInventoryReader
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		quotes: mocks.NewMockDocumentQuoteReader(t), repairOrders: mocks.NewMockDocumentRepairOrderReader(t),
		settlements: mocks.NewMockDocumentSettlementReader(t), stockMovements: mocks.NewMockDocumentStockMovementReader(t),
		revenue: mocks.NewMockDocumentRevenueReader(t), garages: mocks.NewMockDocumentGarageReader(t),
		inventory: mocks.NewMockDocumentInventoryReader(t),
	}

	uc := usecase.NewDocumentUseCase(e.quotes, e.repairOrders, e.settlements, e.stockMovements, e.revenue, e.garages, e.inventory)
	e.srv = handler.NewServer(uc)

	return e
}

func defaultGarage() usecase.GarageHeaderInfo {
	return usecase.GarageHeaderInfo{Name: "District 1 Garage", Address: "123 Le Loi", Phone: "028-1234-5678"}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 10)
	assert.Equal(t, access.InventoryRead, p[documentv1.DocumentService_ExportGoodsReceiptPdf_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryRead, p[documentv1.DocumentService_ExportStockIssuePdf_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryCostRead, p[documentv1.DocumentService_ExportPurchasesExcel_FullMethodName].Garage.Permission)
	assert.Equal(t, access.InventoryCostRead, p[documentv1.DocumentService_ExportStockValuationExcel_FullMethodName].Garage.Permission)
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

		notFound := apperror.Wrap(apperror.CodeNotFound, "quote not found", errDocumentTestBoom)
		e.quotes.EXPECT().Get(anyArg, garageID, quoteID).Return(usecase.QuoteInfo{}, notFound).Once()

		_, err := e.srv.ExportQuotePdf(t.Context(), &documentv1.ExportQuotePdfRequest{GarageId: garageID, QuoteId: quoteID})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestExportRepairOrderPdf(t *testing.T) {
	t.Parallel()

	t.Run("returns a PDF HttpBody", func(t *testing.T) {
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
	})

	t.Run("repair order not found", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		notFound := apperror.Wrap(apperror.CodeNotFound, "repair order not found", errDocumentTestBoom)
		e.repairOrders.EXPECT().Get(anyArg, garageID, repairOrderID).Return(usecase.RepairOrderInfo{}, notFound).Once()

		_, err := e.srv.ExportRepairOrderPdf(t.Context(), &documentv1.ExportRepairOrderPdfRequest{
			GarageId: garageID, RepairOrderId: repairOrderID,
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("garage not found", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		e.repairOrders.EXPECT().Get(anyArg, garageID, repairOrderID).Return(usecase.RepairOrderInfo{
			Number: "LSC-0001", CustomerName: "An Nguyen", TotalAmount: 250_000,
			Lines:  []render.DocumentLine{{Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000}},
			Issuer: issuer.Issuer{},
		}, nil).Once()

		notFound := apperror.Wrap(apperror.CodeNotFound, "garage not found", errDocumentTestBoom)
		e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageHeaderInfo{}, notFound).Once()

		_, err := e.srv.ExportRepairOrderPdf(t.Context(), &documentv1.ExportRepairOrderPdfRequest{
			GarageId: garageID, RepairOrderId: repairOrderID,
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}

func TestExportSettlementReceiptPdf(t *testing.T) {
	t.Parallel()

	t.Run("returns a PDF HttpBody", func(t *testing.T) {
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
	})

	t.Run("settlement not found", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		notFound := apperror.Wrap(apperror.CodeNotFound, "settlement not found", errDocumentTestBoom)
		e.settlements.EXPECT().Get(anyArg, garageID, settlementID).Return(usecase.SettlementInfo{}, notFound).Once()

		_, err := e.srv.ExportSettlementReceiptPdf(t.Context(), &documentv1.ExportSettlementReceiptPdfRequest{
			GarageId: garageID, SettlementId: settlementID,
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("garage not found", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		e.settlements.EXPECT().Get(anyArg, garageID, settlementID).Return(usecase.SettlementInfo{
			Number: "QT-0001", CustomerName: "An Nguyen", TotalAmount: 1_000_000,
			Issuer: issuer.Issuer{},
		}, nil).Once()

		notFound := apperror.Wrap(apperror.CodeNotFound, "garage not found", errDocumentTestBoom)
		e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageHeaderInfo{}, notFound).Once()

		_, err := e.srv.ExportSettlementReceiptPdf(t.Context(), &documentv1.ExportSettlementReceiptPdfRequest{
			GarageId: garageID, SettlementId: settlementID,
		})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}

func TestExportStockMovementsExcel(t *testing.T) {
	t.Parallel()

	t.Run("returns an Excel HttpBody", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.stockMovements.EXPECT().List(anyArg, garageID, itemID).Return([]render.StockMovementRow{
			{Date: "2026-09-30", SKU: "OIL-5W30", ItemName: "Engine oil 5W30", Direction: "in", Quantity: "10", Reference: "SR-1"},
		}, nil).Once()

		resp, err := e.srv.ExportStockMovementsExcel(t.Context(), &documentv1.ExportStockMovementsExcelRequest{
			GarageId: garageID, PartId: itemID,
		})
		require.NoError(t, err)
		assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", resp.GetContentType())
		assert.NotEmpty(t, resp.GetData())
	})

	t.Run("item not found", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		notFound := apperror.Wrap(apperror.CodeNotFound, "item not found", errDocumentTestBoom)
		e.stockMovements.EXPECT().List(anyArg, garageID, itemID).Return(nil, notFound).Once()

		_, err := e.srv.ExportStockMovementsExcel(t.Context(), &documentv1.ExportStockMovementsExcelRequest{
			GarageId: garageID, PartId: itemID,
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestExportRevenueExcel(t *testing.T) {
	t.Parallel()

	req := func(period reportingv1.RevenuePeriod, from, to string) *documentv1.ExportRevenueExcelRequest {
		return &documentv1.ExportRevenueExcelRequest{GarageId: garageID, Period: period, From: from, To: to}
	}

	t.Run("returns an Excel HttpBody", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		from, to := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		e.revenue.EXPECT().Revenue(anyArg, garageID, "month", from, to).Return(usecase.RevenueInfo{
			Points: []render.RevenueRow{{Period: "2026-09", Amount: 100}}, TotalAmount: 100,
		}, nil).Once()

		resp, err := e.srv.ExportRevenueExcel(t.Context(), req(reportingv1.RevenuePeriod_REVENUE_PERIOD_MONTH, "2026-09-01", "2026-10-01"))
		require.NoError(t, err)
		assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", resp.GetContentType())
		assert.NotEmpty(t, resp.GetData())
	})

	t.Run("maps every period", func(t *testing.T) {
		t.Parallel()

		for period, want := range map[reportingv1.RevenuePeriod]string{
			reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY:         "day",
			reportingv1.RevenuePeriod_REVENUE_PERIOD_QUARTER:     "quarter",
			reportingv1.RevenuePeriod_REVENUE_PERIOD_YEAR:        "year",
			reportingv1.RevenuePeriod_REVENUE_PERIOD_UNSPECIFIED: "",
		} {
			e := newEnv(t)
			e.revenue.EXPECT().Revenue(anyArg, garageID, want, anyArg, anyArg).Return(usecase.RevenueInfo{}, nil).Once()

			_, err := e.srv.ExportRevenueExcel(t.Context(), req(period, "2026-09-01", "2026-10-01"))
			require.NoError(t, err)
		}
	})

	t.Run("rejects a bad date", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		_, err := e.srv.ExportRevenueExcel(t.Context(), req(reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, "nope", "2026-10-01"))
		assert.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = e.srv.ExportRevenueExcel(t.Context(), req(reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, "2026-09-01", "nope"))
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("reader failure", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.revenue.EXPECT().Revenue(anyArg, garageID, "day", anyArg, anyArg).Return(usecase.RevenueInfo{}, errDocumentTestBoom).Once()

		_, err := e.srv.ExportRevenueExcel(t.Context(), req(reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY, "2026-09-01", "2026-10-01"))
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}

func TestExportPaymentVoucherPdf(t *testing.T) {
	t.Parallel()

	req := &documentv1.ExportPaymentVoucherPdfRequest{GarageId: garageID, SettlementId: settlementID, PaymentId: "p-1"}

	t.Run("renders the voucher from its own issuer snapshot", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.settlements.EXPECT().Get(anyArg, garageID, settlementID).Return(usecase.SettlementInfo{
			Number: "QT-0001",
			Vouchers: []usecase.VoucherInfo{{
				ID: "p-1", Number: "PT-0001", Date: "12/10/2026", PayerName: "Nguyễn An", Amount: 1_000_000,
				Issuer: issuer.Issuer{Name: "Công ty An Phát"},
			}},
		}, nil).Once()

		resp, err := e.srv.ExportPaymentVoucherPdf(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "application/pdf", resp.GetContentType())
		assert.NotEmpty(t, resp.GetData())
	})

	t.Run("unknown payment", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.settlements.EXPECT().Get(anyArg, garageID, settlementID).Return(usecase.SettlementInfo{}, nil).Once()

		_, err := e.srv.ExportPaymentVoucherPdf(t.Context(), req)
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("unknown settlement", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		notFound := apperror.Wrap(apperror.CodeNotFound, "settlement not found", errDocumentTestBoom)
		e.settlements.EXPECT().Get(anyArg, garageID, settlementID).Return(usecase.SettlementInfo{}, notFound).Once()

		_, err := e.srv.ExportPaymentVoucherPdf(t.Context(), req)
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

func TestExportInventoryDocuments(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Twice()
	e.inventory.EXPECT().GoodsReceipt(anyArg, garageID, "r-1").Return(usecase.GoodsReceiptInfo{Number: "PN-1"}, nil).Once()
	e.inventory.EXPECT().StockIssue(anyArg, garageID, repairOrderID).Return(usecase.StockIssueInfo{Number: "PX-1"}, nil).Once()
	e.inventory.EXPECT().Purchases(anyArg, garageID, "s-1", "2026-10-01", "2026-11-01").Return(nil, int64(0), nil).Once()
	e.inventory.EXPECT().Valuation(anyArg, garageID, "2026-10-15").Return(nil, int64(0), nil).Once()

	receipt, err := e.srv.ExportGoodsReceiptPdf(t.Context(), &documentv1.ExportGoodsReceiptPdfRequest{GarageId: garageID, GoodsReceiptId: "r-1"})
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", receipt.GetContentType())

	issue, err := e.srv.ExportStockIssuePdf(t.Context(), &documentv1.ExportStockIssuePdfRequest{GarageId: garageID, RepairOrderId: repairOrderID})
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", issue.GetContentType())

	purchases, err := e.srv.ExportPurchasesExcel(t.Context(), &documentv1.ExportPurchasesExcelRequest{
		GarageId: garageID, SupplierId: "s-1", From: "2026-10-01", To: "2026-11-01",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, purchases.GetData())

	valuation, err := e.srv.ExportStockValuationExcel(t.Context(), &documentv1.ExportStockValuationExcelRequest{GarageId: garageID, At: "2026-10-15"})
	require.NoError(t, err)
	assert.NotEmpty(t, valuation.GetData())

	e.inventory.EXPECT().GoodsReceipt(anyArg, garageID, "r-2").Return(usecase.GoodsReceiptInfo{}, apperror.New(apperror.CodeNotFound, "gone")).Once()
	e.inventory.EXPECT().StockIssue(anyArg, garageID, "ro-2").Return(usecase.StockIssueInfo{}, apperror.New(apperror.CodeNotFound, "gone")).Once()
	e.inventory.EXPECT().Purchases(anyArg, garageID, "", "", "").Return(nil, int64(0), apperror.New(apperror.CodeNotFound, "gone")).Once()
	e.inventory.EXPECT().Valuation(anyArg, garageID, "").Return(nil, int64(0), apperror.New(apperror.CodeNotFound, "gone")).Once()

	_, err = e.srv.ExportGoodsReceiptPdf(t.Context(), &documentv1.ExportGoodsReceiptPdfRequest{GarageId: garageID, GoodsReceiptId: "r-2"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.ExportStockIssuePdf(t.Context(), &documentv1.ExportStockIssuePdfRequest{GarageId: garageID, RepairOrderId: "ro-2"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.ExportPurchasesExcel(t.Context(), &documentv1.ExportPurchasesExcelRequest{GarageId: garageID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.ExportStockValuationExcel(t.Context(), &documentv1.ExportStockValuationExcelRequest{GarageId: garageID})
	assert.Equal(t, codes.NotFound, status.Code(err))
}
