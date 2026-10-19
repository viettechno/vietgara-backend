// Package handler exposes the Catalogue module (parts, units, categories and
// part images, FR-CAT-01..09) as a gRPC service, served as REST through
// grpc-gateway. Authorization happens in the request middleware from the
// policies below.
package handler

import (
	"context"
	"math"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// Update-mask paths shared by the units, categories and parts.
const pathName = "name"

// Server implements cataloguev1.CatalogueServiceServer.
type Server struct {
	cataloguev1.UnimplementedCatalogueServiceServer

	catalogue *usecase.UseCase
}

// NewServer wires the handler to its use case.
func NewServer(catalogue *usecase.UseCase) *Server {
	return &Server{catalogue: catalogue}
}

// Policies declares the permission every RPC requires in its garage.
func Policies() middleware.Policies {
	read, write := access.Requires(access.CatalogueRead), access.Requires(access.CatalogueWrite)

	return middleware.Policies{
		cataloguev1.CatalogueService_ListParts_FullMethodName:    middleware.GarageMethod(read),
		cataloguev1.CatalogueService_GetPart_FullMethodName:      middleware.GarageMethod(read),
		cataloguev1.CatalogueService_CreatePart_FullMethodName:   middleware.GarageMethod(write),
		cataloguev1.CatalogueService_UpdatePart_FullMethodName:   middleware.GarageMethod(write),
		cataloguev1.CatalogueService_DeletePart_FullMethodName:   middleware.GarageMethod(write),
		cataloguev1.CatalogueService_ActivatePart_FullMethodName: middleware.GarageMethod(write),
		cataloguev1.CatalogueService_SetPartUnits_FullMethodName: middleware.GarageMethod(write),
		// Barcodes are scanned while handling stock, so they follow the
		// Inventory permissions (FR-INV-13).
		cataloguev1.CatalogueService_SetPartBarcodes_FullMethodName: middleware.GarageMethod(access.Requires(access.InventoryWrite)),
		cataloguev1.CatalogueService_LookupPartByBarcode_FullMethodName: middleware.GarageMethod(
			access.Requires(access.InventoryRead),
		),
		cataloguev1.CatalogueService_AddPartImage_FullMethodName:       middleware.GarageMethod(write),
		cataloguev1.CatalogueService_RemovePartImage_FullMethodName:    middleware.GarageMethod(write),
		cataloguev1.CatalogueService_ListUnits_FullMethodName:          middleware.GarageMethod(read),
		cataloguev1.CatalogueService_CreateUnit_FullMethodName:         middleware.GarageMethod(write),
		cataloguev1.CatalogueService_UpdateUnit_FullMethodName:         middleware.GarageMethod(write),
		cataloguev1.CatalogueService_DeleteUnit_FullMethodName:         middleware.GarageMethod(write),
		cataloguev1.CatalogueService_ListPartCategories_FullMethodName: middleware.GarageMethod(read),
		cataloguev1.CatalogueService_CreatePartCategory_FullMethodName: middleware.GarageMethod(write),
		cataloguev1.CatalogueService_UpdatePartCategory_FullMethodName: middleware.GarageMethod(write),
		cataloguev1.CatalogueService_DeletePartCategory_FullMethodName: middleware.GarageMethod(write),
	}
}

// deleteOutcome runs a delete-or-deactivate use case and maps its outcome.
func deleteOutcome(
	ctx context.Context, del func(context.Context, usecase.DeleteRequest) (lifecycle.Outcome, error),
	garageID, id, actorID string, dryRun bool,
) (commonv1.DeleteOutcome, error) {
	outcome, err := del(ctx, usecase.DeleteRequest{GarageID: garageID, ID: id, ActorID: actorID, DryRun: dryRun})
	if err != nil {
		return commonv1.DeleteOutcome_DELETE_OUTCOME_UNSPECIFIED, err
	}

	return httpapi.DeleteOutcome(outcome), nil
}

// run calls fn with the caller's user id and converts use case errors to gRPC
// status errors.
func run[T any](ctx context.Context, fn func(actorID string) (T, error)) (T, error) {
	resp, err := fn(middleware.UserID(ctx))
	if err != nil {
		var zero T

		return zero, apperror.ToGRPCError(err)
	}

	return resp, nil
}

func badMask(path string) error {
	return apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated")
}

func toInt32(n int) int32 {
	return int32(min(n, math.MaxInt32)) //nolint:gosec // clamped
}

func pageRequest(page, pageSize int32) usecase.PageRequest {
	return usecase.PageRequest{Page: int(page), PageSize: int(pageSize)}
}

func paginationToProto[T any](p *usecase.Page[T]) *commonv1.Pagination {
	return &commonv1.Pagination{Total: toInt32(p.Total), Page: toInt32(p.Page), PageSize: toInt32(p.PageSize)}
}

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

func unitToProto(u *domain.Unit) *cataloguev1.Unit {
	return &cataloguev1.Unit{
		Id: u.ID, GarageId: u.GarageID, Name: u.Name, Abbreviation: u.Abbreviation, IsActive: u.IsActive,
		CreatedAt: converter.TimeToProto(u.CreatedAt), UpdatedAt: converter.TimeToProto(u.UpdatedAt),
	}
}

func categoryToProto(c *domain.Category) *cataloguev1.PartCategory {
	return &cataloguev1.PartCategory{
		Id: c.ID, GarageId: c.GarageID, ParentId: c.ParentID, Name: c.Name, IsActive: c.IsActive,
		CreatedAt: converter.TimeToProto(c.CreatedAt), UpdatedAt: converter.TimeToProto(c.UpdatedAt),
	}
}

func partToProto(p *domain.Part) *cataloguev1.Part {
	out := &cataloguev1.Part{
		Id: p.ID, GarageId: p.GarageID, Sku: p.SKU, Name: p.Name,
		BaseUnitId: p.BaseUnitID, BaseUnitName: p.BaseUnitName, BaseUnitAbbreviation: p.BaseUnitAbbreviation,
		SalePrice: p.SalePrice, CategoryId: p.CategoryID, CategoryName: p.CategoryName, Brand: p.Brand,
		IsActive: p.IsActive, ReorderThreshold: p.ReorderThreshold,
		PackagingUnits: make([]*cataloguev1.PackagingUnit, 0, len(p.PackagingUnits)),
		Barcodes:       make([]*cataloguev1.PartBarcode, 0, len(p.Barcodes)),
		Images:         make([]*cataloguev1.PartImage, 0, len(p.Images)),
		CreatedAt:      converter.TimeToProto(p.CreatedAt), UpdatedAt: converter.TimeToProto(p.UpdatedAt),
	}

	for _, u := range p.PackagingUnits {
		price := u.EffectivePrice
		out.PackagingUnits = append(out.PackagingUnits, &cataloguev1.PackagingUnit{
			UnitId: u.UnitID, Factor: u.Factor, SalePrice: &price,
			UnitName: u.UnitName, UnitAbbreviation: u.UnitAbbreviation, SalePriceOverridden: u.SalePrice != nil,
		})
	}

	for _, b := range p.Barcodes {
		out.Barcodes = append(out.Barcodes, barcodeToProto(b))
	}

	for _, img := range p.Images {
		out.Images = append(out.Images, &cataloguev1.PartImage{FileId: img.FileID, Url: img.URL})
	}

	return out
}

func barcodeToProto(b domain.PartBarcode) *cataloguev1.PartBarcode {
	return &cataloguev1.PartBarcode{
		Barcode: b.Barcode, UnitId: b.UnitID, UnitName: b.UnitName, UnitAbbreviation: b.UnitAbbreviation,
	}
}

// ---------------------------------------------------------------------------
// Units
// ---------------------------------------------------------------------------

// ListUnits lists a garage's units of measure.
func (s *Server) ListUnits(ctx context.Context, req *cataloguev1.ListUnitsRequest) (*cataloguev1.ListUnitsResponse, error) {
	return run(ctx, func(string) (*cataloguev1.ListUnitsResponse, error) {
		page, err := s.catalogue.ListUnits(ctx, usecase.ListUnitsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(),
			IncludeInactive: req.GetIncludeInactive(),
		})
		if err != nil {
			return nil, err
		}

		resp := &cataloguev1.ListUnitsResponse{Data: make([]*cataloguev1.Unit, 0, len(page.Items)), Pagination: paginationToProto(page)}
		for _, u := range page.Items {
			resp.Data = append(resp.Data, unitToProto(u))
		}

		return resp, nil
	})
}

// CreateUnit adds a unit of measure.
func (s *Server) CreateUnit(ctx context.Context, req *cataloguev1.CreateUnitRequest) (*cataloguev1.CreateUnitResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.CreateUnitResponse, error) {
		in := req.GetUnit()

		created, err := s.catalogue.CreateUnit(ctx, usecase.CreateUnitRequest{
			GarageID: req.GetGarageId(), ActorID: actorID,
			Fields: usecase.UnitFields{Name: new(in.GetName()), Abbreviation: new(in.GetAbbreviation())},
		})
		if err != nil {
			return nil, err
		}

		return &cataloguev1.CreateUnitResponse{Unit: unitToProto(created)}, nil
	})
}

// UpdateUnit changes the fields named by the update mask.
func (s *Server) UpdateUnit(ctx context.Context, req *cataloguev1.UpdateUnitRequest) (*cataloguev1.UpdateUnitResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.UpdateUnitResponse, error) {
		in := req.GetUnit()
		update := usecase.UpdateUnitRequest{GarageID: req.GetGarageId(), UnitID: req.GetUnitId(), ActorID: actorID}

		paths := req.GetUpdateMask().GetPaths()
		if len(paths) == 0 {
			paths = []string{pathName, "abbreviation"}
		}

		for _, path := range paths {
			switch path {
			case pathName:
				update.Fields.Name = new(in.GetName())
			case "abbreviation":
				update.Fields.Abbreviation = new(in.GetAbbreviation())
			case "is_active":
				update.Fields.IsActive = new(in.GetIsActive())
			default:
				return nil, badMask(path)
			}
		}

		updated, err := s.catalogue.UpdateUnit(ctx, update)
		if err != nil {
			return nil, err
		}

		return &cataloguev1.UpdateUnitResponse{Unit: unitToProto(updated)}, nil
	})
}

// DeleteUnit deletes or deactivates a unit (FR-CAT-07).
func (s *Server) DeleteUnit(ctx context.Context, req *cataloguev1.DeleteUnitRequest) (*cataloguev1.DeleteUnitResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.DeleteUnitResponse, error) {
		outcome, err := deleteOutcome(ctx, s.catalogue.DeleteUnit, req.GetGarageId(), req.GetUnitId(), actorID, req.GetDryRun())
		if err != nil {
			return nil, err
		}

		return &cataloguev1.DeleteUnitResponse{Outcome: outcome}, nil
	})
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

// ListPartCategories returns the category tree as a flat list.
func (s *Server) ListPartCategories(ctx context.Context, req *cataloguev1.ListPartCategoriesRequest) (*cataloguev1.ListPartCategoriesResponse, error) {
	return run(ctx, func(string) (*cataloguev1.ListPartCategoriesResponse, error) {
		list, err := s.catalogue.ListCategories(ctx, req.GetGarageId(), req.GetIncludeInactive())
		if err != nil {
			return nil, err
		}

		resp := &cataloguev1.ListPartCategoriesResponse{Data: make([]*cataloguev1.PartCategory, 0, len(list))}
		for _, c := range list {
			resp.Data = append(resp.Data, categoryToProto(c))
		}

		return resp, nil
	})
}

// CreatePartCategory adds a category.
func (s *Server) CreatePartCategory(ctx context.Context, req *cataloguev1.CreatePartCategoryRequest) (*cataloguev1.CreatePartCategoryResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.CreatePartCategoryResponse, error) {
		in := req.GetCategory()

		created, err := s.catalogue.CreateCategory(ctx, usecase.CreateCategoryRequest{
			GarageID: req.GetGarageId(), ActorID: actorID,
			Fields: usecase.CategoryFields{Name: new(in.GetName()), SetParent: true, ParentID: in.GetParentId()},
		})
		if err != nil {
			return nil, err
		}

		return &cataloguev1.CreatePartCategoryResponse{Category: categoryToProto(created)}, nil
	})
}

// UpdatePartCategory renames or moves a category, or (de)activates it.
func (s *Server) UpdatePartCategory(ctx context.Context, req *cataloguev1.UpdatePartCategoryRequest) (*cataloguev1.UpdatePartCategoryResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.UpdatePartCategoryResponse, error) {
		in := req.GetCategory()
		update := usecase.UpdateCategoryRequest{GarageID: req.GetGarageId(), CategoryID: req.GetCategoryId(), ActorID: actorID}

		paths := req.GetUpdateMask().GetPaths()
		if len(paths) == 0 {
			paths = []string{pathName, "parent_id"}
		}

		for _, path := range paths {
			switch path {
			case pathName:
				update.Fields.Name = new(in.GetName())
			case "parent_id":
				update.Fields.SetParent, update.Fields.ParentID = true, in.GetParentId()
			case "is_active":
				update.Fields.IsActive = new(in.GetIsActive())
			default:
				return nil, badMask(path)
			}
		}

		updated, err := s.catalogue.UpdateCategory(ctx, update)
		if err != nil {
			return nil, err
		}

		return &cataloguev1.UpdatePartCategoryResponse{Category: categoryToProto(updated)}, nil
	})
}

// DeletePartCategory deletes or deactivates a category (FR-CAT-07).
func (s *Server) DeletePartCategory(ctx context.Context, req *cataloguev1.DeletePartCategoryRequest) (*cataloguev1.DeletePartCategoryResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.DeletePartCategoryResponse, error) {
		outcome, err := deleteOutcome(ctx, s.catalogue.DeleteCategory, req.GetGarageId(), req.GetCategoryId(), actorID, req.GetDryRun())
		if err != nil {
			return nil, err
		}

		return &cataloguev1.DeletePartCategoryResponse{Outcome: outcome}, nil
	})
}

// ---------------------------------------------------------------------------
// Parts
// ---------------------------------------------------------------------------

// ListParts lists a garage's parts.
func (s *Server) ListParts(ctx context.Context, req *cataloguev1.ListPartsRequest) (*cataloguev1.ListPartsResponse, error) {
	return run(ctx, func(string) (*cataloguev1.ListPartsResponse, error) {
		page, err := s.catalogue.ListParts(ctx, usecase.ListPartsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(),
			Query: req.GetQuery(), CategoryID: req.GetCategoryId(), Brand: req.GetBrand(),
			IncludeInactive: req.GetIncludeInactive(),
		})
		if err != nil {
			return nil, err
		}

		resp := &cataloguev1.ListPartsResponse{Data: make([]*cataloguev1.Part, 0, len(page.Items)), Pagination: paginationToProto(page)}
		for _, p := range page.Items {
			resp.Data = append(resp.Data, partToProto(p))
		}

		return resp, nil
	})
}

// GetPart returns one part.
func (s *Server) GetPart(ctx context.Context, req *cataloguev1.GetPartRequest) (*cataloguev1.GetPartResponse, error) {
	return run(ctx, func(string) (*cataloguev1.GetPartResponse, error) {
		p, err := s.catalogue.GetPart(ctx, req.GetGarageId(), req.GetPartId())
		if err != nil {
			return nil, err
		}

		return &cataloguev1.GetPartResponse{Part: partToProto(p)}, nil
	})
}

// CreatePart adds a part; its packaging units are set separately.
func (s *Server) CreatePart(ctx context.Context, req *cataloguev1.CreatePartRequest) (*cataloguev1.CreatePartResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.CreatePartResponse, error) {
		in := req.GetPart()

		fields := usecase.PartFields{
			SKU: new(in.GetSku()), Name: new(in.GetName()), BaseUnitID: new(in.GetBaseUnitId()),
			SalePrice: new(in.GetSalePrice()), Brand: new(in.GetBrand()), ReorderThreshold: new(in.GetReorderThreshold()),
		}

		if in.CategoryId != nil {
			fields.CategoryID = in.CategoryId
		}

		created, err := s.catalogue.CreatePart(ctx, usecase.CreatePartRequest{GarageID: req.GetGarageId(), ActorID: actorID, Fields: fields})
		if err != nil {
			return nil, err
		}

		return &cataloguev1.CreatePartResponse{Part: partToProto(created)}, nil
	})
}

// UpdatePart changes the fields named by the update mask.
func (s *Server) UpdatePart(ctx context.Context, req *cataloguev1.UpdatePartRequest) (*cataloguev1.UpdatePartResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.UpdatePartResponse, error) {
		in := req.GetPart()
		update := usecase.UpdatePartRequest{GarageID: req.GetGarageId(), PartID: req.GetPartId(), ActorID: actorID}

		paths := req.GetUpdateMask().GetPaths()
		if len(paths) == 0 {
			paths = []string{"sku", pathName, "base_unit_id", "sale_price", "category_id", "brand", "reorder_threshold"}
		}

		for _, path := range paths {
			switch path {
			case "sku":
				update.Fields.SKU = new(in.GetSku())
			case pathName:
				update.Fields.Name = new(in.GetName())
			case "base_unit_id":
				update.Fields.BaseUnitID = new(in.GetBaseUnitId())
			case "sale_price":
				update.Fields.SalePrice = new(in.GetSalePrice())
			case "category_id":
				update.Fields.CategoryID = new(in.GetCategoryId())
			case "brand":
				update.Fields.Brand = new(in.GetBrand())
			case "is_active":
				update.Fields.IsActive = new(in.GetIsActive())
			case "reorder_threshold":
				update.Fields.ReorderThreshold = new(in.GetReorderThreshold())
			default:
				return nil, badMask(path)
			}
		}

		updated, err := s.catalogue.UpdatePart(ctx, update)
		if err != nil {
			return nil, err
		}

		return &cataloguev1.UpdatePartResponse{Part: partToProto(updated)}, nil
	})
}

// DeletePart deletes or deactivates a part (FR-CAT-07).
func (s *Server) DeletePart(ctx context.Context, req *cataloguev1.DeletePartRequest) (*cataloguev1.DeletePartResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.DeletePartResponse, error) {
		outcome, err := deleteOutcome(ctx, s.catalogue.DeletePart, req.GetGarageId(), req.GetPartId(), actorID, req.GetDryRun())
		if err != nil {
			return nil, err
		}

		return &cataloguev1.DeletePartResponse{Outcome: outcome}, nil
	})
}

// ActivatePart reactivates a deactivated part.
func (s *Server) ActivatePart(ctx context.Context, req *cataloguev1.ActivatePartRequest) (*cataloguev1.ActivatePartResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.ActivatePartResponse, error) {
		p, err := s.catalogue.ActivatePart(ctx, req.GetGarageId(), req.GetPartId(), actorID)
		if err != nil {
			return nil, err
		}

		return &cataloguev1.ActivatePartResponse{Part: partToProto(p)}, nil
	})
}

// SetPartUnits replaces the part's packaging table.
func (s *Server) SetPartUnits(ctx context.Context, req *cataloguev1.SetPartUnitsRequest) (*cataloguev1.SetPartUnitsResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.SetPartUnitsResponse, error) {
		units := make([]domain.PackagingUnit, 0, len(req.GetPackagingUnits()))

		for _, u := range req.GetPackagingUnits() {
			units = append(units, domain.PackagingUnit{UnitID: u.GetUnitId(), Factor: u.GetFactor(), SalePrice: u.SalePrice})
		}

		p, err := s.catalogue.SetPartUnits(ctx, req.GetGarageId(), req.GetPartId(), actorID, units)
		if err != nil {
			return nil, err
		}

		return &cataloguev1.SetPartUnitsResponse{Part: partToProto(p)}, nil
	})
}

// SetPartBarcodes replaces the part's barcode table.
func (s *Server) SetPartBarcodes(ctx context.Context, req *cataloguev1.SetPartBarcodesRequest) (*cataloguev1.SetPartBarcodesResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.SetPartBarcodesResponse, error) {
		barcodes := make([]domain.PartBarcode, 0, len(req.GetBarcodes()))

		for _, b := range req.GetBarcodes() {
			barcodes = append(barcodes, domain.PartBarcode{Barcode: b.GetBarcode(), UnitID: b.UnitId})
		}

		p, err := s.catalogue.SetPartBarcodes(ctx, req.GetGarageId(), req.GetPartId(), actorID, barcodes)
		if err != nil {
			return nil, err
		}

		return &cataloguev1.SetPartBarcodesResponse{Part: partToProto(p)}, nil
	})
}

// LookupPartByBarcode finds the part a scanned code belongs to.
func (s *Server) LookupPartByBarcode(
	ctx context.Context, req *cataloguev1.LookupPartByBarcodeRequest,
) (*cataloguev1.LookupPartByBarcodeResponse, error) {
	return run(ctx, func(string) (*cataloguev1.LookupPartByBarcodeResponse, error) {
		p, barcode, err := s.catalogue.LookupPartByBarcode(ctx, req.GetGarageId(), req.GetBarcode())
		if err != nil {
			return nil, err
		}

		return &cataloguev1.LookupPartByBarcodeResponse{Part: partToProto(p), Barcode: barcodeToProto(*barcode)}, nil
	})
}

// AddPartImage attaches an uploaded image to the part.
func (s *Server) AddPartImage(ctx context.Context, req *cataloguev1.AddPartImageRequest) (*cataloguev1.AddPartImageResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.AddPartImageResponse, error) {
		p, err := s.catalogue.AddPartImage(ctx, req.GetGarageId(), req.GetPartId(), req.GetFileId(), actorID)
		if err != nil {
			return nil, err
		}

		return &cataloguev1.AddPartImageResponse{Part: partToProto(p)}, nil
	})
}

// RemovePartImage detaches an image from the part.
func (s *Server) RemovePartImage(ctx context.Context, req *cataloguev1.RemovePartImageRequest) (*cataloguev1.RemovePartImageResponse, error) {
	return run(ctx, func(actorID string) (*cataloguev1.RemovePartImageResponse, error) {
		p, err := s.catalogue.RemovePartImage(ctx, req.GetGarageId(), req.GetPartId(), req.GetFileId(), actorID)
		if err != nil {
			return nil, err
		}

		return &cataloguev1.RemovePartImageResponse{Part: partToProto(p)}, nil
	})
}
