package main

import (
	"context"
	"errors"

	catalogueDomain "github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	catalogueHandler "github.com/viettechno/vietgara-backend/internal/catalogue/handler"
	catalogueRepository "github.com/viettechno/vietgara-backend/internal/catalogue/repository"
	catalogueUsecase "github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	fileDomain "github.com/viettechno/vietgara-backend/internal/file/domain"
	fileRepository "github.com/viettechno/vietgara-backend/internal/file/repository"
	fileUsecase "github.com/viettechno/vietgara-backend/internal/file/usecase"
	inventoryRepository "github.com/viettechno/vietgara-backend/internal/inventory/repository"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
	"github.com/viettechno/vietgara-backend/pkg/partquantity"
)

// catalogueModule is the wired Catalogue module (FRD 3.21, Release 6.0):
// parts, units of measure and packaging, the category tree and part images.
type catalogueModule struct {
	server *catalogueHandler.Server
	jobs   *catalogueHandler.JobServer
	uc     *catalogueUsecase.UseCase
	repo   *catalogueRepository.PostgresRepository
}

func buildCatalogue(pool *platformdb.Pool, files *fileModule, usage *catalogueUsage) *catalogueModule {
	repo := catalogueRepository.NewPostgresRepository(pool)
	uc := catalogueUsecase.NewUseCase(repo, usage, catalogueFiles{files: fileRepository.NewPostgresRepository(pool), uc: files.uc})

	return &catalogueModule{server: catalogueHandler.NewServer(uc), jobs: catalogueHandler.NewJobServer(uc), uc: uc, repo: repo}
}

// catalogueUsage tells the Catalogue whether stock movements or documents
// refer to a part or a unit (the delete-or-deactivate rule, FR-CAT-07, and
// the frozen base unit), without Catalogue importing those modules. It is
// filled in once they are built, since they in turn convert quantities
// through the Catalogue.
type catalogueUsage struct {
	inventory    *inventoryRepository.PostgresRepository
	quotes       *quoteRepository.PostgresRepository
	repairOrders *repairorderRepository.PostgresRepository
}

func (u *catalogueUsage) PartInUse(ctx context.Context, garageID, partID string) (bool, error) {
	return u.inUse(ctx, garageID, partID, "")
}

func (u *catalogueUsage) UnitInUse(ctx context.Context, garageID, unitID string) (bool, error) {
	return u.inUse(ctx, garageID, "", unitID)
}

// JobInUse reports whether a quote or repair order line came from the job.
func (u *catalogueUsage) JobInUse(ctx context.Context, garageID, jobID string) (bool, error) {
	return lifecycle.AnyInUse(ctx,
		func(ctx context.Context) (bool, error) { return u.quotes.LinesFromJob(ctx, garageID, jobID) },
		func(ctx context.Context) (bool, error) { return u.repairOrders.LinesFromJob(ctx, garageID, jobID) },
	)
}

func (u *catalogueUsage) inUse(ctx context.Context, garageID, partID, unitID string) (bool, error) {
	return lifecycle.AnyInUse(ctx,
		func(ctx context.Context) (bool, error) {
			return u.inventory.MovementsReference(ctx, garageID, partID, unitID)
		},
		func(ctx context.Context) (bool, error) { return u.quotes.LinesReference(ctx, garageID, partID, unitID) },
		func(ctx context.Context) (bool, error) {
			return u.repairOrders.LinesReference(ctx, garageID, partID, unitID)
		},
	)
}

// catalogueFiles gives the Catalogue its part images over the File module.
type catalogueFiles struct {
	files *fileRepository.PostgresRepository
	uc    *fileUsecase.UseCase
}

func (f catalogueFiles) PartImage(ctx context.Context, garageID, partID, fileID string) error {
	file, err := f.files.FindByID(ctx, garageID, fileID)
	if err != nil {
		if errors.Is(err, fileDomain.ErrFileNotFound) {
			return catalogueDomain.ErrNotPartImage
		}

		return err
	}

	if file.Kind != fileDomain.KindPartImage || !file.IsActive || file.EntityType != fileDomain.EntityPart || file.EntityID != partID {
		return catalogueDomain.ErrNotPartImage
	}

	return nil
}

func (f catalogueFiles) URLs(ctx context.Context, garageID string, fileIDs []string) (map[string]string, error) {
	urls := make(map[string]string, len(fileIDs))

	for _, id := range fileIDs {
		file, err := f.files.FindByID(ctx, garageID, id)
		if err != nil {
			if errors.Is(err, fileDomain.ErrFileNotFound) {
				continue
			}

			return nil, err
		}

		urls[id] = f.uc.PublicURL(file.Key)
	}

	return urls, nil
}

func (f catalogueFiles) Release(ctx context.Context, garageID, fileID, actorID string) error {
	_, err := f.uc.Delete(ctx, fileUsecase.DeleteRequest{GarageID: garageID, FileID: fileID, ActorID: actorID})

	return err
}

// partQuantities converts quantities typed in a packaging unit for the
// modules that hold them (stock receipts, quote and repair-order part
// lines).
type partQuantities struct {
	uc *catalogueUsecase.UseCase
}

func (p partQuantities) resolve(ctx context.Context, garageID, partID, unitID, entered string) (catalogueUsecase.Quantity, error) {
	return p.uc.ResolveQuantity(ctx, garageID, partID, unitID, entered)
}

// ResolveQuantity implements partquantity.Resolver.
func (p partQuantities) ResolveQuantity(ctx context.Context, garageID, partID, unitID, entered string) (partquantity.Resolved, error) {
	q, err := p.resolve(ctx, garageID, partID, unitID, entered)
	if err != nil {
		return partquantity.Resolved{}, err
	}

	return partquantity.Resolved{Base: q.Base, UnitID: q.UnitID, Entered: q.Entered, UnitAbbreviation: q.UnitAbbreviation}, nil
}

// inventoryParts gives Inventory its view of the Catalogue.
type inventoryParts struct {
	partQuantities
}

func (p inventoryParts) ResolveQuantity(ctx context.Context, garageID, partID, unitID, entered string) (inventoryUsecase.ResolvedQuantity, error) {
	q, err := p.resolve(ctx, garageID, partID, unitID, entered)
	if err != nil {
		return inventoryUsecase.ResolvedQuantity{}, err
	}

	return inventoryUsecase.ResolvedQuantity{Base: q.Base, UnitID: q.UnitID, Entered: q.Entered}, nil
}

func (p inventoryParts) Parts(ctx context.Context, garageID string, ids []string) ([]inventoryUsecase.PartInfo, error) {
	parts, err := p.uc.PartsByIDs(ctx, garageID, ids)
	if err != nil {
		return nil, err
	}

	return partInfos(parts), nil
}

func (p inventoryParts) SearchParts(ctx context.Context, garageID string, q inventoryUsecase.PartQuery) ([]inventoryUsecase.PartInfo, int, error) {
	page, err := p.uc.ListParts(ctx, catalogueUsecase.ListPartsRequest{
		PageRequest: catalogueUsecase.PageRequest{Page: q.Offset/max(q.Limit, 1) + 1, PageSize: q.Limit},
		GarageID:    garageID, Query: q.Query, CategoryID: q.CategoryID, Brand: q.Brand, IncludeInactive: q.IncludeInactive,
	})
	if err != nil {
		return nil, 0, err
	}

	return partInfos(page.Items), page.Total, nil
}

func (p inventoryParts) UnitAbbreviations(ctx context.Context, garageID string, ids []string) (map[string]string, error) {
	return p.uc.UnitAbbreviations(ctx, garageID, ids)
}

func partInfos(parts []*catalogueDomain.Part) []inventoryUsecase.PartInfo {
	out := make([]inventoryUsecase.PartInfo, 0, len(parts))

	for _, p := range parts {
		info := inventoryUsecase.PartInfo{
			ID: p.ID, SKU: p.SKU, Name: p.Name, BaseUnitName: p.BaseUnitName, BaseUnitAbbreviation: p.BaseUnitAbbreviation,
			CategoryName: p.CategoryName, Brand: p.Brand, IsActive: p.IsActive, ReorderThreshold: p.ReorderThreshold,
		}

		if p.CategoryID != nil {
			info.CategoryID = *p.CategoryID
		}

		if len(p.Images) > 0 {
			info.ThumbnailURL = p.Images[0].URL
		}

		out = append(out, info)
	}

	return out
}
