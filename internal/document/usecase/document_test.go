package usecase_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/render"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	garageID     = "11111111-1111-1111-1111-111111111111"
	quoteID      = "22222222-2222-2222-2222-222222222222"
	settlementID = "33333333-3333-3333-3333-333333333333"
	itemID       = "44444444-4444-4444-4444-444444444444"
)

var errBoom = errors.New("boom")

// deps are the mocked dependencies of one DocumentUseCase under test.
type deps struct {
	Quotes         *mocks.MockDocumentQuoteReader
	Settlements    *mocks.MockDocumentSettlementReader
	StockMovements *mocks.MockDocumentStockMovementReader
	Garages        *mocks.MockDocumentGarageReader
}

func newUseCase(t *testing.T) (*usecase.DocumentUseCase, *deps) {
	t.Helper()

	d := &deps{
		Quotes: mocks.NewMockDocumentQuoteReader(t), Settlements: mocks.NewMockDocumentSettlementReader(t),
		StockMovements: mocks.NewMockDocumentStockMovementReader(t), Garages: mocks.NewMockDocumentGarageReader(t),
	}

	uc := usecase.NewDocumentUseCase(d.Quotes, d.Settlements, d.StockMovements, d.Garages)

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
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageHeaderInfo{}, errBoom).Once()

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("quote not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()

		notFound := apperror.Wrap(apperror.CodeNotFound, "quote not found", errBoom)
		d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(usecase.QuoteInfo{}, notFound).Once()

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID)
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
			TotalAmount: 1_000_000, CustomerAmount: 600_000, InsurerAmount: 400_000, InsurerName: "ABC Insurance",
			Payments: []render.PaymentRow{{Date: "2026-09-30", PayerType: "customer", Method: "cash", Amount: 600_000}},
		}, nil).Once()

		pdf, err := uc.SettlementReceiptPDF(t.Context(), garageID, settlementID)
		require.NoError(t, err)
		require.NotEmpty(t, pdf)
		require.Equal(t, "%PDF", string(pdf[:4]))
	})

	t.Run("settlement not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()

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
