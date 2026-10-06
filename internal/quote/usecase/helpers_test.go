package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	garageID   = "11111111-1111-1111-1111-111111111111"
	customerID = "22222222-2222-2222-2222-222222222222"
	vehicleID  = "33333333-3333-3333-3333-333333333333"
	quoteID    = "44444444-4444-4444-4444-444444444444"
	actorID    = "55555555-5555-5555-5555-555555555555"
	ownerID    = "66666666-6666-6666-6666-666666666666"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC)
)

// passTx is a Transactor that runs fn immediately, ignoring scope --
// enough for the public quote flow, which opens its own transaction (see
// internal/tenant/tenanttest.PassTx for the same pattern).
type passTx struct{}

func (passTx) WithTransaction(ctx context.Context, _ platformdb.Scope, fn func(context.Context) error) error {
	return fn(ctx)
}

// deps are the mocked dependencies of one QuoteUseCase under test.
type deps struct {
	Quotes       *mocks.MockQuoteRepository
	Vehicles     *mocks.MockVehicleReader
	Customers    *mocks.MockQuoteCustomerReader
	Garages      *mocks.MockGarageReader
	Limits       *mocks.MockTransactionLimiter
	Sequences    *mocks.MockSequencer
	RepairOrders *mocks.MockRepairOrderCreator
	Mailer       *mocks.MockQuoteMailer
	Events       *mocks.MockQuoteEventPublisher
}

func newUseCase(t *testing.T) (*usecase.QuoteUseCase, *deps) {
	t.Helper()

	d := &deps{
		Quotes: mocks.NewMockQuoteRepository(t), Vehicles: mocks.NewMockVehicleReader(t),
		Customers: mocks.NewMockQuoteCustomerReader(t), Garages: mocks.NewMockGarageReader(t),
		Limits: mocks.NewMockTransactionLimiter(t), Sequences: mocks.NewMockSequencer(t),
		RepairOrders: mocks.NewMockRepairOrderCreator(t), Mailer: mocks.NewMockQuoteMailer(t),
		Events: mocks.NewMockQuoteEventPublisher(t),
	}

	uc := usecase.NewQuoteUseCase(usecase.Deps{
		Quotes: d.Quotes, Vehicles: d.Vehicles, Customers: d.Customers, Garages: d.Garages, Limits: d.Limits,
		Sequences: d.Sequences, RepairOrders: d.RepairOrders, Mailer: d.Mailer, Events: d.Events, Tx: passTx{},
		AppBaseURL: "https://vietgara.viettechno.com/", Now: func() time.Time { return now },
	})

	return uc, d
}

func defaultGarage() usecase.GarageInfo {
	return usecase.GarageInfo{
		OwnerAccountID: ownerID, Timezone: "Asia/Ho_Chi_Minh", QuoteNumberTemplate: "BG-{YYYY}{MM}-{SEQ:4}",
		RepairOrderNumberTemplate: "LSC-{YYYY}{MM}-{SEQ:4}", DefaultQuoteApprovalMode: domain.ApprovalModeLink, Name: "QA Gara",
	}
}

func oneLine() []usecase.LineInput {
	return []usecase.LineInput{{ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1", UnitPrice: 250_000}}
}

func existingQuote(status domain.Status) *domain.Quote {
	return &domain.Quote{
		ID: quoteID, GarageID: garageID, CustomerID: customerID, CustomerName: "An Nguyen", VehicleID: vehicleID,
		VehicleLicensePlate: "51F-123.45", Number: "BG-202609-0001", Status: status, ApprovalMode: domain.ApprovalModeLink,
		Lines: []domain.Line{
			{ID: "line-1", Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000, Decision: domain.DecisionPending},
			{ID: "line-2", Position: 2, ItemType: domain.ItemTypePart, Description: "Air filter", Quantity: "1", UnitPrice: 480_000, LineTotal: 480_000, Decision: domain.DecisionPending},
		},
		TotalAmount: 730_000, CreatedAt: now, UpdatedAt: now,
	}
}

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}
