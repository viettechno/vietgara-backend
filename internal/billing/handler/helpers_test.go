package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/viettechno/vietgara-backend/internal/billing/handler"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const (
	garageID      = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	repairOrderID = "11111111-1111-1111-1111-111111111111"
	settlementID  = "22222222-2222-2222-2222-222222222222"
	ownerID       = "44444444-4444-4444-4444-444444444444"
	actorID       = "55555555-5555-5555-5555-555555555555"
)

var anyArg = mock.Anything

type env struct {
	srv          *handler.Server
	settlements  *mocks.MockBillingRepository
	repairOrders *mocks.MockBillingRepairOrderReader
	garages      *mocks.MockBillingGarageReader
	collectors   *mocks.MockBillingCollectorReader
	limits       *mocks.MockBillingTransactionLimiter
	sequences    *mocks.MockBillingSequencer
	events       *mocks.MockBillingEventPublisher
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		settlements: mocks.NewMockBillingRepository(t), repairOrders: mocks.NewMockBillingRepairOrderReader(t),
		garages: mocks.NewMockBillingGarageReader(t), collectors: mocks.NewMockBillingCollectorReader(t),
		limits: mocks.NewMockBillingTransactionLimiter(t), sequences: mocks.NewMockBillingSequencer(t),
		events: mocks.NewMockBillingEventPublisher(t),
	}

	// Auditing is the use case's concern (its own tests check the entries).
	auditLog := mocks.NewMockBillingAuditLogger(t)
	auditLog.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Maybe()

	uc := usecase.NewSettlementUseCase(usecase.Deps{
		Settlements: e.settlements, RepairOrders: e.repairOrders, Garages: e.garages, Collectors: e.collectors,
		Limits: e.limits, Sequences: e.sequences, Events: e.events, Audit: auditLog,
	})
	e.srv = handler.NewServer(uc)

	return e
}

func defaultGarage() usecase.GarageInfo {
	return usecase.GarageInfo{OwnerUserID: ownerID, Timezone: "Asia/Ho_Chi_Minh", SettlementNumberTemplate: "QT-{SEQ:4}", VoucherNumberTemplate: "PT-{SEQ:4}"}
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = actorID

	return middleware.WithClaims(t.Context(), claims)
}
