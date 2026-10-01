package main

import (
	inventoryHandler "github.com/viettechno/vietgara-backend/internal/inventory/handler"
	inventoryRepository "github.com/viettechno/vietgara-backend/internal/inventory/repository"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// inventoryModule is the wired Inventory module (FRD 3.7, Release 1.3):
// items, stock receipts and stock-on-hand, plus the repair_order.closed
// consumer that deducts stock for a closed repair order's part lines
// (FR-INV-02).
type inventoryModule struct {
	server *inventoryHandler.Server
	uc     *inventoryUsecase.InventoryUseCase
}

func buildInventory(pool *platformdb.Pool, events *outbox.EventPublisher) *inventoryModule {
	uc := inventoryUsecase.NewInventoryUseCase(inventoryRepository.NewPostgresRepository(pool), events)

	return &inventoryModule{server: inventoryHandler.NewServer(uc), uc: uc}
}
