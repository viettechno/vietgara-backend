package usecase_test

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/render"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
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

var errBoom = errors.New("boom")

// deps are the mocked dependencies of one DocumentUseCase under test.
type deps struct {
	Quotes         *mocks.MockDocumentQuoteReader
	RepairOrders   *mocks.MockDocumentRepairOrderReader
	Settlements    *mocks.MockDocumentSettlementReader
	StockMovements *mocks.MockDocumentStockMovementReader
	Revenue        *mocks.MockDocumentRevenueReader
	Garages        *mocks.MockDocumentGarageReader
}

func newUseCase(t *testing.T) (*usecase.DocumentUseCase, *deps) {
	t.Helper()

	d := &deps{
		Quotes: mocks.NewMockDocumentQuoteReader(t), RepairOrders: mocks.NewMockDocumentRepairOrderReader(t),
		Settlements: mocks.NewMockDocumentSettlementReader(t), StockMovements: mocks.NewMockDocumentStockMovementReader(t),
		Revenue: mocks.NewMockDocumentRevenueReader(t), Garages: mocks.NewMockDocumentGarageReader(t),
	}

	uc := usecase.NewDocumentUseCase(d.Quotes, d.RepairOrders, d.Settlements, d.StockMovements, d.Revenue, d.Garages)

	return uc, d
}

func defaultGarage() usecase.GarageHeaderInfo {
	return usecase.GarageHeaderInfo{Name: "District 1 Garage", Address: "123 Le Loi", Phone: "028-1234-5678"}
}

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}

func TestDocumentUseCase_QuotePDF(t *testing.T) {
	t.Parallel()

	t.Run("renders a non-empty PDF", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(usecase.QuoteInfo{
			Number: "QT-202609-0001", Status: "approved", CustomerName: "An Nguyen", VehiclePlate: "51F-123.45",
			Lines:       []render.DocumentLine{{Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000}},
			TotalAmount: 250_000,
		}, nil).Once()

		pdf, err := uc.QuotePDF(t.Context(), garageID, quoteID)
		require.NoError(t, err)
		require.NotEmpty(t, pdf)
		require.Equal(t, "%PDF", string(pdf[:4]))
	})

	t.Run("garage read failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(usecase.QuoteInfo{Number: "QT-1"}, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageHeaderInfo{}, errBoom).Once()

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("quote not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)

		notFound := apperror.Wrap(apperror.CodeNotFound, "quote not found", errBoom)
		d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(usecase.QuoteInfo{}, notFound).Once()

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestDocumentUseCase_RepairOrderPDF(t *testing.T) {
	t.Parallel()

	t.Run("renders a non-empty PDF", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(usecase.RepairOrderInfo{
			Number: "LSC-202609-0001", Status: "open", CustomerName: "An Nguyen", VehiclePlate: "51F-123.45",
			Lines:       []render.DocumentLine{{Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000}},
			TotalAmount: 250_000,
		}, nil).Once()

		pdf, err := uc.RepairOrderPDF(t.Context(), garageID, repairOrderID)
		require.NoError(t, err)
		require.NotEmpty(t, pdf)
		require.Equal(t, "%PDF", string(pdf[:4]))
	})

	t.Run("repair order not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)

		notFound := apperror.Wrap(apperror.CodeNotFound, "repair order not found", errBoom)
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(usecase.RepairOrderInfo{}, notFound).Once()

		_, err := uc.RepairOrderPDF(t.Context(), garageID, repairOrderID)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestDocumentUseCase_SettlementReceiptPDF(t *testing.T) {
	t.Parallel()

	t.Run("renders a non-empty PDF", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(usecase.SettlementInfo{
			Number: "QT-202609-0001", Status: "paid", CustomerName: "An Nguyen", VehiclePlate: "51F-123.45",
			TotalAmount: 1_000_000, PaidAmount: 600_000,
			Payments: []render.PaymentRow{{Date: "2026-09-30", Method: "cash", Amount: 600_000}},
		}, nil).Once()

		pdf, err := uc.SettlementReceiptPDF(t.Context(), garageID, settlementID)
		require.NoError(t, err)
		require.NotEmpty(t, pdf)
		require.Equal(t, "%PDF", string(pdf[:4]))
	})

	t.Run("settlement not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)

		notFound := apperror.Wrap(apperror.CodeNotFound, "settlement not found", errBoom)
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(usecase.SettlementInfo{}, notFound).Once()

		_, err := uc.SettlementReceiptPDF(t.Context(), garageID, settlementID)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestDocumentUseCase_StockMovementsExcel(t *testing.T) {
	t.Parallel()

	t.Run("renders a non-empty workbook", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.StockMovements.EXPECT().List(mock.Anything, garageID, itemID).Return([]render.StockMovementRow{
			{Date: "2026-09-30", SKU: "OIL-5W30", ItemName: "Engine oil 5W30", Direction: "in", Quantity: "10", Reference: "SR-1"},
		}, nil).Once()

		workbook, err := uc.StockMovementsExcel(t.Context(), garageID, itemID)
		require.NoError(t, err)
		require.NotEmpty(t, workbook)
		require.Equal(t, "PK", string(workbook[:2]))
	})

	t.Run("reader failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.StockMovements.EXPECT().List(mock.Anything, garageID, itemID).Return(nil, errBoom).Once()

		_, err := uc.StockMovementsExcel(t.Context(), garageID, itemID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestDocumentUseCase_RevenueExcel(t *testing.T) {
	t.Parallel()

	from, to := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	t.Run("renders a non-empty workbook", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Revenue.EXPECT().Revenue(mock.Anything, garageID, "month", from, to).Return(usecase.RevenueInfo{
			Points: []render.RevenueRow{{Period: "2026-09", Amount: 1_500_000}}, TotalAmount: 1_500_000,
		}, nil).Once()

		workbook, err := uc.RevenueExcel(t.Context(), garageID, "month", from, to)
		require.NoError(t, err)
		require.Equal(t, "PK", string(workbook[:2]))
	})

	t.Run("reader failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Revenue.EXPECT().Revenue(mock.Anything, garageID, "month", from, to).Return(usecase.RevenueInfo{}, errBoom).Once()

		_, err := uc.RevenueExcel(t.Context(), garageID, "month", from, to)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

// TestDocumentUseCase_Letterhead covers FR-TEN-06: a document prints the
// legal identity it was issued with, not the garage's current profile.
func TestDocumentUseCase_Letterhead(t *testing.T) {
	t.Parallel()

	snapshot := issuer.Issuer{
		Name: "Cong ty TNHH An Phat", Address: "12 Nguyen Hue", Phone: "0281234567", TaxCode: "0301234567",
		Representative: "Nguyen Van An", BankName: "VCB", AccountNumber: "001", LogoKey: "garages/g/logo/g/l.png",
	}
	doc := usecase.QuoteInfo{Number: "BG-1", Status: "draft", Issuer: snapshot}

	t.Run("a document with a snapshot ignores the garage's current profile", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(doc, nil).Once()
		d.Garages.EXPECT().Logo(mock.Anything, snapshot.LogoKey).Return(logoPNG(t), nil).Once()

		pdf, err := uc.QuotePDF(t.Context(), garageID, quoteID)
		require.NoError(t, err)
		require.Equal(t, "%PDF", string(pdf[:4]))
	})

	t.Run("a logo that cannot be read leaves the document without it", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(doc, nil).Once()
		d.Garages.EXPECT().Logo(mock.Anything, snapshot.LogoKey).Return(nil, errBoom).Once()

		pdf, err := uc.QuotePDF(t.Context(), garageID, quoteID)
		require.NoError(t, err)
		require.Equal(t, "%PDF", string(pdf[:4]))
	})

	t.Run("a snapshot without a logo reads none", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		noLogo := doc
		noLogo.Issuer.LogoKey = ""
		d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(noLogo, nil).Once()

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID)
		require.NoError(t, err)
	})

	t.Run("a document without a snapshot falls back to the current profile", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(usecase.QuoteInfo{Number: "BG-1"}, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID)
		require.NoError(t, err)
	})
}

func logoPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}
