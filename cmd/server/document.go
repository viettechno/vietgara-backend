package main

import (
	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	documentHandler "github.com/viettechno/vietgara-backend/internal/document/handler"
	documentUsecase "github.com/viettechno/vietgara-backend/internal/document/usecase"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// documentModule is the wired Document/Print module (FRD 3.13, Release
// 1.4): PDF export for a quote and a settlement receipt (FR-DOC-01),
// Excel export for the stock movement report (FR-DOC-02). It owns no
// tables of its own -- every document is rendered fresh from its own
// narrow reads of the owning module's data.
type documentModule struct {
	server *documentHandler.Server
}

func buildDocument(
	quotes *quoteRepository.PostgresRepository,
	settlements *billingUsecase.SettlementUseCase,
	inventory *inventoryUsecase.InventoryUseCase,
	garages *tenantRepo.PostgresGarageRepository,
) *documentModule {
	uc := documentUsecase.NewDocumentUseCase(
		documentQuotes{quotes: quotes}, documentSettlements{uc: settlements}, documentStockMovements{uc: inventory},
		documentGarages{garages: garages},
	)

	return &documentModule{server: documentHandler.NewServer(uc)}
}
