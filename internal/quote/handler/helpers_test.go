package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/quote/handler"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
)

const (
	garageID   = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	quoteID    = "11111111-1111-1111-1111-111111111111"
	customerID = "22222222-2222-2222-2222-222222222222"
	vehicleID  = "33333333-3333-3333-3333-333333333333"
	ownerID    = "44444444-4444-4444-4444-444444444444"
	actorID    = "55555555-5555-5555-5555-555555555555"
)

var anyArg = mock.Anything

type env struct {
	srv       *handler.Server
	quotes    *mocks.MockQuoteRepository
	vehicles  *mocks.MockVehicleReader
	customers *mocks.MockQuoteCustomerReader
	garages   *mocks.MockGarageReader
	limits    *mocks.MockTransactionLimiter
	sequences *mocks.MockSequencer
	repairs   *mocks.MockRepairOrderCreator
	mailer    *mocks.MockQuoteMailer
	events    *mocks.MockQuoteEventPublisher
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		quotes: mocks.NewMockQuoteRepository(t), vehicles: mocks.NewMockVehicleReader(t),
		customers: mocks.NewMockQuoteCustomerReader(t), garages: mocks.NewMockGarageReader(t),
		limits: mocks.NewMockTransactionLimiter(t), sequences: mocks.NewMockSequencer(t),
		repairs: mocks.NewMockRepairOrderCreator(t), mailer: mocks.NewMockQuoteMailer(t),
		events: mocks.NewMockQuoteEventPublisher(t),
	}

	uc := usecase.NewQuoteUseCase(usecase.Deps{
		Quotes: e.quotes, Vehicles: e.vehicles, Customers: e.customers, Garages: e.garages, Limits: e.limits,
		Sequences: e.sequences, RepairOrders: e.repairs, Mailer: e.mailer, Events: e.events, Tx: passTx{},
		AppBaseURL: "https://vietgara.viettechno.com",
	})
	e.srv = handler.NewServer(uc)

	return e
}

type passTx struct{}

func (passTx) WithTransaction(ctx context.Context, _ platformdb.Scope, fn func(context.Context) error) error {
	return fn(ctx)
}

func garageInfoWithRepairOrderTemplate() usecase.GarageInfo {
	return usecase.GarageInfo{
		OwnerUserID: ownerID, Timezone: "Asia/Ho_Chi_Minh", QuoteNumberTemplate: "BG-{SEQ:4}",
		RepairOrderNumberTemplate: "LSC-{SEQ:4}", DefaultQuoteApprovalMode: "link",
	}
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = actorID

	return middleware.WithClaims(t.Context(), claims)
}
