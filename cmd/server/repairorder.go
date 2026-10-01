package main

import (
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/numbering"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	repairorderHandler "github.com/viettechno/vietgara-backend/internal/repairorder/handler"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// repairOrderModule is the wired Repair Order module (FRD 3.6, Release
// 1.3): the Kanban board, technician assignment and closing, plus the
// draft-creation-from-quote use case Quote already depended on since
// Release 1.2 (FR-QUO-04).
type repairOrderModule struct {
	server *repairorderHandler.Server
	uc     *repairorderUsecase.RepairOrderUseCase
}

func buildRepairOrder(
	pool *platformdb.Pool,
	events *outbox.EventPublisher,
	garages *tenantRepo.PostgresGarageRepository,
	entitlements *licenseUsecase.EntitlementUseCase,
	vehicles repairOrderVehicles,
) *repairOrderModule {
	uc := repairorderUsecase.NewRepairOrderUseCase(repairorderUsecase.Deps{
		RepairOrders: repairorderRepository.NewPostgresRepository(pool), Vehicles: vehicles,
		Garages: repairOrderGarages{garages: garages}, Limits: entitlements, Sequences: numbering.NewSequencer(pool),
		Events: events,
	})

	return &repairOrderModule{server: repairorderHandler.NewServer(uc), uc: uc}
}
