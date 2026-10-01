package main

import (
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	quoteHandler "github.com/viettechno/vietgara-backend/internal/quote/handler"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	quoteUsecase "github.com/viettechno/vietgara-backend/internal/quote/usecase"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"

	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/numbering"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// quoteModule is the wired Quote & Pricing module (Release 1.2) plus the
// minimal Repair Order module (draft creation only, FR-QUO-04) it depends
// on; the Repair Order Service proper is built in Release 1.3 on the same
// tables and domain types.
type quoteModule struct {
	server       *quoteHandler.Server
	quotes       *quoteRepository.PostgresRepository
	repairOrders *repairorderUsecase.RepairOrderUseCase
}

func buildQuote(
	pool *platformdb.Pool,
	tx *platformdb.TxManager,
	events *outbox.EventPublisher,
	mailer mail.Sender,
	garages *tenantRepo.PostgresGarageRepository,
	entitlements *licenseUsecase.EntitlementUseCase,
	customers *customerRepository.PostgresCustomerRepository,
	vehicles *customerRepository.PostgresVehicleRepository,
) *quoteModule {
	quotesRepo := quoteRepository.NewPostgresRepository(pool)
	repairOrders := repairorderUsecase.NewRepairOrderUseCase(repairorderRepository.NewPostgresRepository(pool))
	customerAdapter := quoteCustomers{customers: customers, vehicles: vehicles}

	quotes := quoteUsecase.NewQuoteUseCase(quoteUsecase.Deps{
		Quotes: quotesRepo, Vehicles: customerAdapter, Customers: customerAdapter,
		Garages: quoteGarages{garages: garages}, Limits: entitlements, Sequences: numbering.NewSequencer(pool),
		RepairOrders: quoteRepairOrders{uc: repairOrders}, Mailer: mailer, Events: events, Tx: tx,
		AppBaseURL: envOr("APP_BASE_URL", envOr("FRONTEND_ORIGIN", "http://localhost:8000")),
	})

	return &quoteModule{server: quoteHandler.NewServer(quotes), quotes: quotesRepo, repairOrders: repairOrders}
}
