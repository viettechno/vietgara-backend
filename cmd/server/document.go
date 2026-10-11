package main

import (
	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	documentHandler "github.com/viettechno/vietgara-backend/internal/document/handler"
	documentUsecase "github.com/viettechno/vietgara-backend/internal/document/usecase"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// documentModule is the wired Document/Print module (FRD 3.13, Release
// 1.4): PDF export for a quote, a repair order and a settlement receipt
// (FR-DOC-01), Excel export for the stock movement report (FR-DOC-02).
// It owns no tables of its own -- every document is rendered fresh from
// its own narrow reads of the owning module's data.
type documentModule struct {
	server *documentHandler.Server
}

func buildDocument(
	quotes *quoteRepository.PostgresRepository,
	repairOrders *repairorderUsecase.RepairOrderUseCase,
	settlements *billingUsecase.SettlementUseCase,
	inventory *inventoryUsecase.InventoryUseCase,
	garages *tenantRepo.PostgresGarageRepository,
	store storage.Store,
) *documentModule {
	uc := documentUsecase.NewDocumentUseCase(
		documentQuotes{quotes: quotes}, documentRepairOrders{uc: repairOrders}, documentSettlements{uc: settlements},
		documentStockMovements{uc: inventory}, documentGarages{garages: garages, store: store},
	)

	return &documentModule{server: documentHandler.NewServer(uc)}
}
