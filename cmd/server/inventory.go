package main

import (
	inventoryHandler "github.com/viettechno/vietgara-backend/internal/inventory/handler"
	inventoryRepository "github.com/viettechno/vietgara-backend/internal/inventory/repository"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// inventoryModule is the wired Inventory module (FRD 3.7, Release 1.3):
// stock on hand, stock receipts and movements, plus the repair_order.closed
// consumer that deducts stock for a closed repair order's part lines
// (FR-INV-02).
type inventoryModule struct {
	server *inventoryHandler.Server
	uc     *inventoryUsecase.InventoryUseCase
	repo   *inventoryRepository.PostgresRepository
}

func buildInventory(pool *platformdb.Pool, events *outbox.EventPublisher, parts inventoryUsecase.PartCatalogue) *inventoryModule {
	repo := inventoryRepository.NewPostgresRepository(pool)
	uc := inventoryUsecase.NewInventoryUseCase(repo, parts, events)

	return &inventoryModule{server: inventoryHandler.NewServer(uc), uc: uc, repo: repo}
}
