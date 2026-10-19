package handler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/handler"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const (
	garageID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	partID   = "11111111-1111-1111-1111-111111111111"
	unitID   = "22222222-2222-2222-2222-222222222222"
	caseID   = "33333333-3333-3333-3333-333333333333"
	catID    = "44444444-4444-4444-4444-444444444444"
	fileID   = "55555555-5555-5555-5555-555555555555"
	actorID  = "66666666-6666-6666-6666-666666666666"
)

var (
	anyArg  = mock.Anything
	errBoom = errors.New("boom")
)

type env struct {
	srv   *handler.Server
	repo  *mocks.MockCatalogueRepository
	usage *mocks.MockCatalogueUsageChecker
	files *mocks.MockCatalogueFiles
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		repo: mocks.NewMockCatalogueRepository(t), usage: mocks.NewMockCatalogueUsageChecker(t),
		files: mocks.NewMockCatalogueFiles(t),
	}
	e.srv = handler.NewServer(usecase.NewUseCase(e.repo, e.usage, e.files))

	return e
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = actorID

	return middleware.WithClaims(t.Context(), claims)
}

func sampleUnit() *domain.Unit {
	return &domain.Unit{ID: unitID, GarageID: garageID, Name: "Can", Abbreviation: "can", IsActive: true}
}

func samplePart() *domain.Part {
	cat := catID

	return &domain.Part{
		ID: partID, GarageID: garageID, SKU: "BEER-330", Name: "Beer", BaseUnitID: unitID, BaseUnitName: "Can",
		BaseUnitAbbreviation: "can", SalePrice: 15_000, CategoryID: &cat, CategoryName: "Beverages", Brand: "Tiger",
		IsActive: true, ReorderThreshold: "48",
		PackagingUnits: []domain.PackagingUnit{
			{UnitID: caseID, Factor: "24", UnitName: "Carton", UnitAbbreviation: "ctn", EffectivePrice: 360_000},
			{UnitID: "x", Factor: "6", SalePrice: new(int64(95_000)), EffectivePrice: 95_000},
		},
		Images: []domain.Image{{FileID: fileID, URL: "https://cdn/a.png"}},
	}
}

func mask(paths ...string) *fieldmaskpb.FieldMask { return &fieldmaskpb.FieldMask{Paths: paths} }

func code(err error) codes.Code { return status.Code(err) }

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 19)
	assert.NotNil(t, p[cataloguev1.CatalogueService_ListParts_FullMethodName].Garage)
	assert.NotNil(t, p[cataloguev1.CatalogueService_DeletePartCategory_FullMethodName].Garage)
}

func TestUnitRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.repo.EXPECT().ListUnits(anyArg, garageID, usecase.UnitFilter{IncludeInactive: true, Limit: 20}).Return([]*domain.Unit{sampleUnit()}, 1, nil).Once()

	list, err := e.srv.ListUnits(ctx, &cataloguev1.ListUnitsRequest{GarageId: garageID, IncludeInactive: true})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)
	assert.Equal(t, "can", list.GetData()[0].GetAbbreviation())
	assert.Equal(t, int32(1), list.GetPagination().GetTotal())

	e.repo.EXPECT().CreateUnit(anyArg, anyArg).Return(nil).Once()

	created, err := e.srv.CreateUnit(ctx, &cataloguev1.CreateUnitRequest{GarageId: garageID, Unit: &cataloguev1.Unit{Name: "Can", Abbreviation: "can"}})
	require.NoError(t, err)
	assert.True(t, created.GetUnit().GetIsActive())

	e.repo.EXPECT().FindUnit(anyArg, garageID, unitID).Return(sampleUnit(), nil).Times(3)
	e.repo.EXPECT().UpdateUnit(anyArg, anyArg).Return(nil).Times(3)

	updated, err := e.srv.UpdateUnit(ctx, &cataloguev1.UpdateUnitRequest{
		GarageId: garageID, UnitId: unitID, Unit: &cataloguev1.Unit{Name: "Tin", Abbreviation: "t"}, UpdateMask: mask("name", "abbreviation"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Tin", updated.GetUnit().GetName())

	updated, err = e.srv.UpdateUnit(ctx, &cataloguev1.UpdateUnitRequest{GarageId: garageID, UnitId: unitID, Unit: &cataloguev1.Unit{Name: "All", Abbreviation: "a"}})
	require.NoError(t, err)
	assert.Equal(t, "All", updated.GetUnit().GetName(), "no mask updates every settable field")

	updated, err = e.srv.UpdateUnit(ctx, &cataloguev1.UpdateUnitRequest{GarageId: garageID, UnitId: unitID, Unit: &cataloguev1.Unit{IsActive: false}, UpdateMask: mask("is_active")})
	require.NoError(t, err)
	assert.False(t, updated.GetUnit().GetIsActive())

	_, err = e.srv.UpdateUnit(ctx, &cataloguev1.UpdateUnitRequest{GarageId: garageID, UnitId: unitID, UpdateMask: mask("id")})
	assert.Equal(t, codes.InvalidArgument, code(err))

	e.repo.EXPECT().FindUnit(anyArg, garageID, unitID).Return(sampleUnit(), nil).Once()
	e.repo.EXPECT().UnitUsedByParts(anyArg, garageID, unitID).Return(true, nil).Once()

	del, err := e.srv.DeleteUnit(ctx, &cataloguev1.DeleteUnitRequest{GarageId: garageID, UnitId: unitID, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, del.GetOutcome())

	e.repo.EXPECT().ListUnits(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()

	_, err = e.srv.ListUnits(ctx, &cataloguev1.ListUnitsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, code(err))

	e.repo.EXPECT().FindUnit(anyArg, garageID, unitID).Return(nil, domain.ErrUnitNotFound).Once()

	_, err = e.srv.DeleteUnit(ctx, &cataloguev1.DeleteUnitRequest{GarageId: garageID, UnitId: unitID})
	assert.Equal(t, codes.NotFound, code(err))
}

func TestCategoryRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)
	root := &domain.Category{ID: catID, GarageID: garageID, Name: "Oil", IsActive: true}

	e.repo.EXPECT().ListCategories(anyArg, garageID, false).Return([]*domain.Category{root}, nil).Once()

	list, err := e.srv.ListPartCategories(signedIn(t), &cataloguev1.ListPartCategoriesRequest{GarageId: garageID})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)
	assert.Nil(t, list.GetData()[0].ParentId)

	e.repo.EXPECT().ListCategories(anyArg, garageID, true).Return([]*domain.Category{root}, nil).Times(4)
	e.repo.EXPECT().CreateCategory(anyArg, anyArg).Return(nil).Once()

	parent := catID
	created, err := e.srv.CreatePartCategory(ctx, &cataloguev1.CreatePartCategoryRequest{
		GarageId: garageID, Category: &cataloguev1.PartCategory{Name: "Synthetic", ParentId: &parent},
	})
	require.NoError(t, err)
	assert.Equal(t, catID, created.GetCategory().GetParentId())

	e.repo.EXPECT().FindCategory(anyArg, garageID, catID).Return(&domain.Category{ID: catID, Name: "Oil", IsActive: true}, nil).Twice()
	e.repo.EXPECT().UpdateCategory(anyArg, anyArg).Return(nil).Twice()

	updated, err := e.srv.UpdatePartCategory(ctx, &cataloguev1.UpdatePartCategoryRequest{
		GarageId: garageID, CategoryId: catID, Category: &cataloguev1.PartCategory{Name: "Lubricants"}, UpdateMask: mask("name", "parent_id", "is_active"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Lubricants", updated.GetCategory().GetName())
	assert.False(t, updated.GetCategory().GetIsActive())

	_, err = e.srv.UpdatePartCategory(ctx, &cataloguev1.UpdatePartCategoryRequest{
		GarageId: garageID, CategoryId: catID, Category: &cataloguev1.PartCategory{Name: "Default mask"},
	})
	require.NoError(t, err)

	_, err = e.srv.UpdatePartCategory(ctx, &cataloguev1.UpdatePartCategoryRequest{GarageId: garageID, CategoryId: catID, UpdateMask: mask("garage_id")})
	assert.Equal(t, codes.InvalidArgument, code(err))

	e.repo.EXPECT().FindCategory(anyArg, garageID, catID).Return(root, nil).Once()
	e.repo.EXPECT().CategoryHasParts(anyArg, garageID, catID).Return(false, nil).Once()

	del, err := e.srv.DeletePartCategory(ctx, &cataloguev1.DeletePartCategoryRequest{GarageId: garageID, CategoryId: catID, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, del.GetOutcome())

	e.repo.EXPECT().ListCategories(anyArg, garageID, false).Return(nil, errBoom).Once()

	_, err = e.srv.ListPartCategories(ctx, &cataloguev1.ListPartCategoriesRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, code(err))
}

func TestPartRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.repo.EXPECT().FindPart(anyArg, garageID, anyArg).Return(samplePart(), nil).Maybe()
	e.files.EXPECT().URLs(anyArg, garageID, anyArg).Return(map[string]string{fileID: "https://cdn/a.png"}, nil).Maybe()

	// List.
	e.repo.EXPECT().ListParts(anyArg, garageID, usecase.PartFilter{Query: "beer", Brand: "Tiger", Limit: 20}).
		Return([]*domain.Part{samplePart()}, 1, nil).Once()

	list, err := e.srv.ListParts(ctx, &cataloguev1.ListPartsRequest{GarageId: garageID, Query: "beer", Brand: "Tiger"})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)

	p := list.GetData()[0]
	assert.Equal(t, "BEER-330", p.GetSku())
	assert.Equal(t, "can", p.GetBaseUnitAbbreviation())
	assert.Equal(t, catID, p.GetCategoryId())
	assert.Equal(t, "48", p.GetReorderThreshold())
	require.Len(t, p.GetPackagingUnits(), 2)
	assert.Equal(t, int64(360_000), p.GetPackagingUnits()[0].GetSalePrice(), "the derived price is reported")
	assert.False(t, p.GetPackagingUnits()[0].GetSalePriceOverridden())
	assert.True(t, p.GetPackagingUnits()[1].GetSalePriceOverridden())
	require.Len(t, p.GetImages(), 1)
	assert.Equal(t, "https://cdn/a.png", p.GetImages()[0].GetUrl())

	// Get.
	got, err := e.srv.GetPart(ctx, &cataloguev1.GetPartRequest{GarageId: garageID, PartId: partID})
	require.NoError(t, err)
	assert.Equal(t, partID, got.GetPart().GetId())

	// Create.
	e.repo.EXPECT().FindUnit(anyArg, garageID, unitID).Return(sampleUnit(), nil).Maybe()
	e.repo.EXPECT().FindCategory(anyArg, garageID, catID).Return(&domain.Category{ID: catID, Name: "Beverages", IsActive: true}, nil).Maybe()
	e.repo.EXPECT().CreatePart(anyArg, anyArg).Return(nil).Once()

	cat := catID
	created, err := e.srv.CreatePart(ctx, &cataloguev1.CreatePartRequest{GarageId: garageID, Part: &cataloguev1.Part{
		Sku: "BEER-330", Name: "Beer", BaseUnitId: unitID, SalePrice: 15_000, CategoryId: &cat, Brand: "Tiger", ReorderThreshold: "48",
	}})
	require.NoError(t, err)
	assert.Equal(t, partID, created.GetPart().GetId())

	e.repo.EXPECT().CreatePart(anyArg, anyArg).Return(domain.ErrSKUTaken).Once()

	_, err = e.srv.CreatePart(ctx, &cataloguev1.CreatePartRequest{GarageId: garageID, Part: &cataloguev1.Part{Sku: "BEER-330", Name: "Beer", BaseUnitId: unitID}})
	assert.Equal(t, codes.AlreadyExists, code(err))

	// Update: every settable path, then the default mask, then an unknown path.
	e.repo.EXPECT().UpdatePart(anyArg, anyArg).Return(nil).Times(2)

	_, err = e.srv.UpdatePart(ctx, &cataloguev1.UpdatePartRequest{
		GarageId: garageID, PartId: partID, UpdateMask: mask("sku", "name", "sale_price", "category_id", "brand", "is_active", "reorder_threshold"),
		Part: &cataloguev1.Part{Sku: "S", Name: "N", SalePrice: 1, Brand: "B", IsActive: true, ReorderThreshold: "2", CategoryId: &cat},
	})
	require.NoError(t, err)

	_, err = e.srv.UpdatePart(ctx, &cataloguev1.UpdatePartRequest{
		GarageId: garageID, PartId: partID, Part: &cataloguev1.Part{Sku: "S", Name: "N", BaseUnitId: unitID},
	})
	require.NoError(t, err)

	_, err = e.srv.UpdatePart(ctx, &cataloguev1.UpdatePartRequest{GarageId: garageID, PartId: partID, UpdateMask: mask("garage_id")})
	assert.Equal(t, codes.InvalidArgument, code(err))

	// Delete and activate.
	e.usage.EXPECT().PartInUse(anyArg, garageID, partID).Return(true, nil).Once()

	del, err := e.srv.DeletePart(ctx, &cataloguev1.DeletePartRequest{GarageId: garageID, PartId: partID, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, del.GetOutcome())

	e.repo.EXPECT().SetPartActive(anyArg, garageID, partID, true, actorID).Return(nil).Once()

	_, err = e.srv.ActivatePart(ctx, &cataloguev1.ActivatePartRequest{GarageId: garageID, PartId: partID})
	require.NoError(t, err)

	// Packaging.
	e.repo.EXPECT().FindUnit(anyArg, garageID, caseID).Return(&domain.Unit{ID: caseID, IsActive: true}, nil).Once()
	e.repo.EXPECT().ReplacePackaging(anyArg, garageID, partID, actorID, anyArg).Return(nil).Once()

	_, err = e.srv.SetPartUnits(ctx, &cataloguev1.SetPartUnitsRequest{
		GarageId: garageID, PartId: partID, PackagingUnits: []*cataloguev1.PackagingUnit{{UnitId: caseID, Factor: "24"}},
	})
	require.NoError(t, err)

	// Images.
	e.repo.EXPECT().ListImages(anyArg, garageID, partID).Return(nil, nil).Once()
	e.files.EXPECT().PartImage(anyArg, garageID, partID, fileID).Return(nil).Once()
	e.repo.EXPECT().FileAttached(anyArg, garageID, fileID).Return(false, nil).Once()
	e.repo.EXPECT().AddImage(anyArg, garageID, partID, fileID, actorID).Return(nil).Once()

	_, err = e.srv.AddPartImage(ctx, &cataloguev1.AddPartImageRequest{GarageId: garageID, PartId: partID, FileId: fileID})
	require.NoError(t, err)

	e.repo.EXPECT().RemoveImage(anyArg, garageID, partID, fileID).Return(nil).Once()
	e.files.EXPECT().Release(anyArg, garageID, fileID, actorID).Return(nil).Once()

	_, err = e.srv.RemovePartImage(ctx, &cataloguev1.RemovePartImageRequest{GarageId: garageID, PartId: partID, FileId: fileID})
	require.NoError(t, err)
}

func TestPartRPCs_Failures(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	e.repo.EXPECT().FindPart(anyArg, garageID, partID).Return(nil, domain.ErrPartNotFound)
	e.repo.EXPECT().ListParts(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()
	e.repo.EXPECT().SetPartActive(anyArg, garageID, partID, true, actorID).Return(domain.ErrPartNotFound).Once()
	e.repo.EXPECT().RemoveImage(anyArg, garageID, partID, fileID).Return(domain.ErrImageNotFound).Once()

	_, err := e.srv.ListParts(ctx, &cataloguev1.ListPartsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, code(err))

	_, err = e.srv.GetPart(ctx, &cataloguev1.GetPartRequest{GarageId: garageID, PartId: partID})
	assert.Equal(t, codes.NotFound, code(err))

	_, err = e.srv.CreatePart(ctx, &cataloguev1.CreatePartRequest{GarageId: garageID, Part: &cataloguev1.Part{}})
	assert.Equal(t, codes.InvalidArgument, code(err))

	_, err = e.srv.UpdatePart(ctx, &cataloguev1.UpdatePartRequest{GarageId: garageID, PartId: partID})
	assert.Equal(t, codes.NotFound, code(err))

	_, err = e.srv.DeletePart(ctx, &cataloguev1.DeletePartRequest{GarageId: garageID, PartId: partID})
	assert.Equal(t, codes.NotFound, code(err))

	_, err = e.srv.ActivatePart(ctx, &cataloguev1.ActivatePartRequest{GarageId: garageID, PartId: partID})
	assert.Equal(t, codes.NotFound, code(err))

	_, err = e.srv.SetPartUnits(ctx, &cataloguev1.SetPartUnitsRequest{GarageId: garageID, PartId: partID})
	assert.Equal(t, codes.NotFound, code(err))

	_, err = e.srv.AddPartImage(ctx, &cataloguev1.AddPartImageRequest{GarageId: garageID, PartId: partID, FileId: fileID})
	assert.Equal(t, codes.NotFound, code(err))

	_, err = e.srv.RemovePartImage(ctx, &cataloguev1.RemovePartImageRequest{GarageId: garageID, PartId: partID, FileId: fileID})
	assert.Equal(t, codes.NotFound, code(err))
}

func TestCategoryAndUnitFailures(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	_, err := e.srv.CreateUnit(ctx, &cataloguev1.CreateUnitRequest{GarageId: garageID, Unit: &cataloguev1.Unit{}})
	assert.Equal(t, codes.InvalidArgument, code(err))

	e.repo.EXPECT().ListCategories(anyArg, garageID, true).Return(nil, nil).Once()

	_, err = e.srv.CreatePartCategory(ctx, &cataloguev1.CreatePartCategoryRequest{GarageId: garageID, Category: &cataloguev1.PartCategory{}})
	assert.Equal(t, codes.InvalidArgument, code(err))

	e.repo.EXPECT().FindCategory(anyArg, garageID, catID).Return(nil, domain.ErrCategoryNotFound).Twice()

	_, err = e.srv.UpdatePartCategory(ctx, &cataloguev1.UpdatePartCategoryRequest{GarageId: garageID, CategoryId: catID})
	assert.Equal(t, codes.NotFound, code(err))

	_, err = e.srv.DeletePartCategory(ctx, &cataloguev1.DeletePartCategoryRequest{GarageId: garageID, CategoryId: catID})
	assert.Equal(t, codes.NotFound, code(err))
}
