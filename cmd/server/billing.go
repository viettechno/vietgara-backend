package main

import (
	billingHandler "github.com/viettechno/vietgara-backend/internal/billing/handler"
	billingRepository "github.com/viettechno/vietgara-backend/internal/billing/repository"
	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/numbering"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// billingModule is the wired Billing & Settlement module (FRD 3.8/3.9,
// Release 1.4): settlements from a closed repair order and recorded
// payments.
type billingModule struct {
	server      *billingHandler.Server
	uc          *billingUsecase.SettlementUseCase
	settlements *billingRepository.PostgresRepository
}

func buildBilling(
	pool *platformdb.Pool,
	events *outbox.EventPublisher,
	garages *tenantRepo.PostgresGarageRepository,
	users *identityRepo.PostgresUserRepository,
	entitlements *licenseUsecase.EntitlementUseCase,
	repairOrders *repairorderUsecase.RepairOrderUseCase,
	auditLog *audit.Logger,
	billingParties billingUsecase.BillingPartyResolver,
) *billingModule {
	settlementsRepo := billingRepository.NewPostgresRepository(pool)

	uc := billingUsecase.NewSettlementUseCase(billingUsecase.Deps{
		Settlements: settlementsRepo, RepairOrders: billingRepairOrders{uc: repairOrders}, BillingParties: billingParties,
		Garages: billingGarages{garages: garages}, Collectors: billingCollectors{users: users}, Limits: entitlements,
		Sequences: numbering.NewSequencer(pool), Events: events, Audit: auditLog,
	})

	return &billingModule{server: billingHandler.NewServer(uc), uc: uc, settlements: settlementsRepo}
}
