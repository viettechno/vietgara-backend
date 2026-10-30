package main

import (
	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	documentHandler "github.com/viettechno/vietgara-backend/internal/document/handler"
	documentRepository "github.com/viettechno/vietgara-backend/internal/document/repository"
	documentUsecase "github.com/viettechno/vietgara-backend/internal/document/usecase"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	reportingUsecase "github.com/viettechno/vietgara-backend/internal/reporting/usecase"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// documentModule is the wired Document/Print module (FRD 3.13): PDF export
// and printing of the six documents (FR-DOC-01, FR-DOC-04, FR-DOC-05) and
// the Excel reports (FR-DOC-02). Every document is rendered fresh from its
// own narrow reads of the owning module's data; the module keeps only the
// print settings and the print log of a garage (Release 10.0).
type documentModule struct {
	server *documentHandler.Server
	uc     *documentUsecase.DocumentUseCase
}

func buildDocument(
	pool *platformdb.Pool,
	quotes *quoteRepository.PostgresRepository,
	repairOrders *repairorderUsecase.RepairOrderUseCase,
	settlements *billingUsecase.SettlementUseCase,
	inventory *inventoryUsecase.InventoryUseCase,
	reporting *reportingUsecase.ReportingUseCase,
	garages *tenantRepo.PostgresGarageRepository,
	store storage.Store,
) *documentModule {
	uc := documentUsecase.NewDocumentUseCase(
		documentQuotes{quotes: quotes}, documentRepairOrders{uc: repairOrders}, documentSettlements{uc: settlements},
		documentStockMovements{uc: inventory}, documentRevenue{uc: reporting}, documentGarages{garages: garages, store: store},
		documentInventory{uc: inventory}, documentRepository.NewPostgresPrintStore(pool),
	)

	return &documentModule{server: documentHandler.NewServer(uc), uc: uc}
}
