package main

import (
	"context"

	catalogueDomain "github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	catalogueUsecase "github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
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
	"github.com/viettechno/vietgara-backend/pkg/partquantity"
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
	parts partquantity.Resolver,
	jobs quoteUsecase.JobCatalogue,
) *quoteModule {
	quotesRepo := quoteRepository.NewPostgresRepository(pool)
	customerAdapter := quoteCustomers{customers: customers, vehicles: vehicles}

	quotes := quoteUsecase.NewQuoteUseCase(quoteUsecase.Deps{
		Quotes: quotesRepo, Vehicles: customerAdapter, Customers: customerAdapter, BillingParties: billingParties,
		Garages: quoteGarages{garages: garages}, Limits: entitlements, Sequences: numbering.NewSequencer(pool),
		RepairOrders: quoteRepairOrders{uc: repairOrders}, Mailer: mailer, Events: events, Tx: tx,
		AppBaseURL: appBaseURL(), Parts: parts, Jobs: jobs,
	})

	return &quoteModule{server: quoteHandler.NewServer(quotes), quotes: quotesRepo}
}

// quoteJobs gives Quote the job catalogue (FR-CAT-06) over Catalogue: a job
// as the quote needs it, refused when it is unknown or inactive, with the
// suggestions of deactivated parts left out.
type quoteJobs struct {
	jobs *catalogueUsecase.UseCase
}

// Job implements quoteUsecase.JobCatalogue.
func (q quoteJobs) Job(ctx context.Context, garageID, jobID string) (quoteUsecase.Job, error) {
	job, err := q.jobs.GetJob(ctx, garageID, jobID)
	if err != nil {
		return quoteUsecase.Job{}, err
	}

	if !job.IsActive {
		return quoteUsecase.Job{}, catalogueUsecase.ToAppError("add quote job", catalogueDomain.ErrJobInactive)
	}

	out := quoteUsecase.Job{ID: job.ID, Name: job.Name, LaborPrice: job.LaborPrice}

	for _, p := range job.Parts {
		if !p.PartIsActive {
			continue
		}

		part := quoteUsecase.JobPart{
			PartID: p.PartID, Description: p.PartName, Quantity: p.Quantity, UnitPrice: p.BasePrice, NonBillable: p.NonBillable,
		}
		if p.UnitID != nil {
			part.UnitID = *p.UnitID
		}

		out.Parts = append(out.Parts, part)
	}

	return out, nil
}
