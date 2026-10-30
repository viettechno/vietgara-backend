package main

import (
	inventoryHandler "github.com/viettechno/vietgara-backend/internal/inventory/handler"
	inventoryRepository "github.com/viettechno/vietgara-backend/internal/inventory/repository"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/numbering"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// inventoryModule is the wired Inventory module (FRD 3.7): stock on hand
// and movements, goods receipt documents with average cost (FR-INV-06/07),
// the usual suppliers of a part (FR-INV-19) and the stock issue of each
// repair order (FR-INV-20), kept by the repair_order.* consumers.
type inventoryModule struct {
	server *inventoryHandler.Server
	uc     *inventoryUsecase.InventoryUseCase
	repo   *inventoryRepository.PostgresRepository
}

func buildInventory(
	pool *platformdb.Pool, events *outbox.EventPublisher, parts inventoryUsecase.PartCatalogue,
	garages inventoryUsecase.Garages, directory inventoryUsecase.Directory, auditLog *audit.Logger,
) *inventoryModule {
	repo := inventoryRepository.NewPostgresRepository(pool).WithEvents(events)
	uc := inventoryUsecase.NewInventoryUseCase(inventoryUsecase.Dependencies{
		Repo: repo, Parts: parts, Events: events, Garages: garages, Directory: directory,
		Sequences: numbering.NewSequencer(pool), Audit: auditLog,
	})

	return &inventoryModule{server: inventoryHandler.NewServer(uc), uc: uc, repo: repo}
}
