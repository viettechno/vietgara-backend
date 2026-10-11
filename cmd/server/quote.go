package main

import (
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	quoteHandler "github.com/viettechno/vietgara-backend/internal/quote/handler"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	quoteUsecase "github.com/viettechno/vietgara-backend/internal/quote/usecase"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"

	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/numbering"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// quoteModule is the wired Quote & Pricing module (Release 1.2).
type quoteModule struct {
	server *quoteHandler.Server
	quotes *quoteRepository.PostgresRepository
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
	repairOrders *repairorderUsecase.RepairOrderUseCase,
	billingParties quoteUsecase.BillingPartyResolver,
) *quoteModule {
	quotesRepo := quoteRepository.NewPostgresRepository(pool)
	customerAdapter := quoteCustomers{customers: customers, vehicles: vehicles}

	quotes := quoteUsecase.NewQuoteUseCase(quoteUsecase.Deps{
		Quotes: quotesRepo, Vehicles: customerAdapter, Customers: customerAdapter, BillingParties: billingParties,
		Garages: quoteGarages{garages: garages}, Limits: entitlements, Sequences: numbering.NewSequencer(pool),
		RepairOrders: quoteRepairOrders{uc: repairOrders}, Mailer: mailer, Events: events, Tx: tx,
		AppBaseURL: appBaseURL(),
	})

	return &quoteModule{server: quoteHandler.NewServer(quotes), quotes: quotesRepo}
}
