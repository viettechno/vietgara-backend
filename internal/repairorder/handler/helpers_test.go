package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/repairorder/handler"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
)

const (
	garageID      = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	repairOrderID = "11111111-1111-1111-1111-111111111111"
	vehicleID     = "33333333-3333-3333-3333-333333333333"
	ownerID       = "44444444-4444-4444-4444-444444444444"
	actorID       = "55555555-5555-5555-5555-555555555555"
)

var anyArg = mock.Anything

type env struct {
	srv          *handler.Server
	repairOrders *mocks.MockRepairOrderRepository
	vehicles     *mocks.MockRepairOrderVehicleReader
	garages      *mocks.MockRepairOrderGarageReader
	limits       *mocks.MockRepairOrderTransactionLimiter
	sequences    *mocks.MockRepairOrderSequencer
	events       *mocks.MockRepairOrderEventPublisher
	issues       *mocks.MockRepairOrderStockIssues
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		repairOrders: mocks.NewMockRepairOrderRepository(t), vehicles: mocks.NewMockRepairOrderVehicleReader(t),
		garages: mocks.NewMockRepairOrderGarageReader(t), limits: mocks.NewMockRepairOrderTransactionLimiter(t),
		sequences: mocks.NewMockRepairOrderSequencer(t), events: mocks.NewMockRepairOrderEventPublisher(t),
		issues: mocks.NewMockRepairOrderStockIssues(t),
	}

	e.repairOrders.EXPECT().Lock(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	uc := usecase.NewRepairOrderUseCase(usecase.Deps{
		RepairOrders: e.repairOrders, Vehicles: e.vehicles, Garages: e.garages, Limits: e.limits,
		Sequences: e.sequences, Events: e.events, StockIssues: e.issues,
	})
	e.srv = handler.NewServer(uc)

	return e
}

func defaultGarage() usecase.GarageInfo {
	return usecase.GarageInfo{OwnerUserID: ownerID, Timezone: "Asia/Ho_Chi_Minh", RepairOrderNumberTemplate: "LSC-{SEQ:4}"}
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = actorID

	return middleware.WithClaims(t.Context(), claims)
}
