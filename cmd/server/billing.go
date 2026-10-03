package main

import (
	billingHandler "github.com/viettechno/vietgara-backend/internal/billing/handler"
	billingRepository "github.com/viettechno/vietgara-backend/internal/billing/repository"
	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/numbering"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// billingModule is the wired Billing & Settlement module (FRD 3.8/3.9,
// Release 1.4): settlements from a closed repair order, the
// customer/insurer allocation, and recorded payments.
type billingModule struct {
	server *billingHandler.Server
	uc     *billingUsecase.SettlementUseCase
}

func buildBilling(
	pool *platformdb.Pool,
	events *outbox.EventPublisher,
	garages *tenantRepo.PostgresGarageRepository,
	entitlements *licenseUsecase.EntitlementUseCase,
	partners *customerRepository.PostgresPartnerRepository,
	repairOrders *repairorderUsecase.RepairOrderUseCase,
) *billingModule {
	uc := billingUsecase.NewSettlementUseCase(billingUsecase.Deps{
		Settlements: billingRepository.NewPostgresRepository(pool), RepairOrders: billingRepairOrders{uc: repairOrders},
		Partners: billingPartners{partners: partners}, Garages: billingGarages{garages: garages}, Limits: entitlements,
		Sequences: numbering.NewSequencer(pool), Events: events,
	})

	return &billingModule{server: billingHandler.NewServer(uc), uc: uc}
}
