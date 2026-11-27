package main

import (
	"context"

	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	catalogueUsecase "github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	documentHandler "github.com/viettechno/vietgara-backend/internal/document/handler"
	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
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

// documentParts reads a part's label from the Catalogue module (FR-INV-13).
type documentParts struct{ uc *catalogueUsecase.UseCase }

// PartLabel returns the part's name, SKU and the barcode to print: the
// base unit's first barcode, or the SKU when it has none.
func (a documentParts) PartLabel(ctx context.Context, garageID, partID string) (printmodel.PartLabel, error) {
	p, err := a.uc.GetPart(ctx, garageID, partID)
	if err != nil {
		return printmodel.PartLabel{}, err //nolint:wrapcheck // already an *apperror.AppError
	}

	label := printmodel.PartLabel{Name: p.Name, SKU: p.SKU, Barcode: p.SKU}

	for _, b := range p.Barcodes {
		if b.UnitID == nil {
			label.Barcode = b.Barcode

			break
		}
	}

	return label, nil
}

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
	catalogue *catalogueUsecase.UseCase,
) *documentModule {
	uc := documentUsecase.NewDocumentUseCase(
		documentQuotes{quotes: quotes}, documentRepairOrders{uc: repairOrders}, documentSettlements{uc: settlements},
		documentStockMovements{uc: inventory}, documentRevenue{uc: reporting}, documentGarages{garages: garages, store: store},
		documentInventory{uc: inventory}, documentRepository.NewPostgresPrintStore(pool),
	).WithPrinting(documentRepository.NewPostgresPrinterStore(pool), documentParts{uc: catalogue})

	return &documentModule{server: documentHandler.NewServer(uc), uc: uc}
}
