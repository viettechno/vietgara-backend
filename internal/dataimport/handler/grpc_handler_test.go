package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/handler"
	"github.com/viettechno/vietgara-backend/internal/dataimport/template"
	"github.com/viettechno/vietgara-backend/internal/dataimport/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

var anyArg = mock.Anything

const (
	garageID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	actorID  = "66666666-6666-6666-6666-666666666666"
)

// stubReader returns a canned sheet.
type stubReader struct{ sheet *usecase.Sheet }

func (s stubReader) Read([]byte) (*usecase.Sheet, error) { return s.sheet, nil }

type env struct {
	srv       *handler.Server
	catalogue *mocks.MockImportCatalogue
	suppliers *mocks.MockImportSuppliers
	runs      *mocks.MockImportRunRepository
}

func newEnv(t *testing.T, sheet *usecase.Sheet) *env {
	t.Helper()

	e := &env{
		catalogue: mocks.NewMockImportCatalogue(t), suppliers: mocks.NewMockImportSuppliers(t),
		runs: mocks.NewMockImportRunRepository(t),
	}

	e.srv = handler.NewServer(usecase.NewUseCase(usecase.Dependencies{
		Catalogue: e.catalogue, Suppliers: e.suppliers, Inventory: mocks.NewMockImportInventory(t),
		Runs: e.runs, Reader: stubReader{sheet: sheet}, Audit: mocks.NewMockImportAuditLogger(t),
	}))

	return e
}

func (e *env) expectNothingExists() {
	e.catalogue.EXPECT().PartsBySKUs(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	e.catalogue.EXPECT().Units(mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	e.catalogue.EXPECT().Categories(mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	e.catalogue.EXPECT().ExistingBarcodes(mock.Anything, mock.Anything, mock.Anything).Return(map[string]string{}, nil).Maybe()
}

func as(t *testing.T, perms ...access.Permission) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = actorID

	return access.WithAccess(middleware.WithClaims(t.Context(), claims), &access.Access{Permissions: perms})
}

func headerOnly(typ domain.Type) *usecase.Sheet {
	headers := template.Columns(typ)
	return &usecase.Sheet{Rows: []usecase.SheetRow{{Number: 1, Cells: headers}}}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 3)

	for _, method := range []string{
		inventoryv1.ImportService_DownloadImportTemplate_FullMethodName,
		inventoryv1.ImportService_PreviewImport_FullMethodName,
		inventoryv1.ImportService_ConfirmImport_FullMethodName,
	} {
		assert.NotNil(t, p[method].Garage, method)
	}
}

func TestDownloadImportTemplate(t *testing.T) {
	t.Parallel()

	e := newEnv(t, nil)

	for _, typ := range []inventoryv1.ImportType{
		inventoryv1.ImportType_IMPORT_TYPE_PARTS, inventoryv1.ImportType_IMPORT_TYPE_GOODS_RECEIPTS,
		inventoryv1.ImportType_IMPORT_TYPE_STOCK_COUNTS,
	} {
		body, err := e.srv.DownloadImportTemplate(t.Context(), &inventoryv1.DownloadImportTemplateRequest{GarageId: garageID, ImportType: typ})
		require.NoError(t, err, typ.String())
		assert.Equal(t, handler.ContentType, body.GetContentType())
		assert.Greater(t, len(body.GetData()), 100, "a workbook, not an empty body")
	}

	_, err := e.srv.DownloadImportTemplate(t.Context(), &inventoryv1.DownloadImportTemplateRequest{GarageId: garageID})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "an unspecified type is refused")
}

func TestPreviewImport(t *testing.T) {
	t.Parallel()

	e := newEnv(t, headerOnly(domain.TypeParts))
	e.expectNothingExists()

	resp, err := e.srv.PreviewImport(as(t, access.CatalogueWrite), &inventoryv1.PreviewImportRequest{
		GarageId: garageID, ImportType: inventoryv1.ImportType_IMPORT_TYPE_PARTS, FileName: "parts.xlsx", Content: []byte("xlsx"),
	})
	require.NoError(t, err)

	preview := resp.GetPreview()
	assert.Equal(t, inventoryv1.ImportType_IMPORT_TYPE_PARTS, preview.GetImportType())
	assert.Equal(t, template.Columns(domain.TypeParts), preview.GetColumns())
	assert.Zero(t, preview.GetTotalRows())
	assert.False(t, preview.GetCanConfirm(), "an empty file cannot be confirmed")
	assert.NotNil(t, preview.GetErrors())

	_, err = e.srv.PreviewImport(as(t), &inventoryv1.PreviewImportRequest{GarageId: garageID})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPreviewReportsAMissingPermissionAsAFileError(t *testing.T) {
	t.Parallel()

	e := newEnv(t, headerOnly(domain.TypeStockCounts))
	e.expectNothingExists()

	resp, err := e.srv.PreviewImport(as(t, access.InventoryRead), &inventoryv1.PreviewImportRequest{
		GarageId: garageID, ImportType: inventoryv1.ImportType_IMPORT_TYPE_STOCK_COUNTS, Content: []byte("xlsx"),
	})
	require.NoError(t, err)

	require.NotEmpty(t, resp.GetPreview().GetErrors())
	assert.Equal(t, "PERMISSION_REQUIRED", resp.GetPreview().GetErrors()[0].GetCode())
	assert.Zero(t, resp.GetPreview().GetErrors()[0].GetRow())
}

func TestConfirmImportRefusals(t *testing.T) {
	t.Parallel()

	t.Run("unsupported type", func(t *testing.T) {
		t.Parallel()

		_, err := newEnv(t, nil).srv.ConfirmImport(as(t), &inventoryv1.ConfirmImportRequest{GarageId: garageID})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("caller lacks the permission of the type", func(t *testing.T) {
		t.Parallel()

		_, err := newEnv(t, nil).srv.ConfirmImport(context.Background(), &inventoryv1.ConfirmImportRequest{
			GarageId: garageID, ImportType: inventoryv1.ImportType_IMPORT_TYPE_PARTS, Content: []byte("xlsx"),
		})
		assert.Equal(t, codes.PermissionDenied, status.Code(err), "no access in the context means no permission")
	})

	t.Run("same file twice", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t, headerOnly(domain.TypeParts))
		e.runs.EXPECT().Find(mock.Anything, garageID, "parts", mock.Anything).Return(&usecase.Run{ID: "run-1"}, nil)

		_, err := e.srv.ConfirmImport(as(t, access.CatalogueWrite), &inventoryv1.ConfirmImportRequest{
			GarageId: garageID, ImportType: inventoryv1.ImportType_IMPORT_TYPE_PARTS, Content: []byte("xlsx"),
		})
		assert.Equal(t, codes.AlreadyExists, status.Code(err))
	})

	t.Run("file with errors", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t, headerOnly(domain.TypeParts))
		e.expectNothingExists()
		e.runs.EXPECT().Find(mock.Anything, garageID, "parts", mock.Anything).Return(nil, nil)

		_, err := e.srv.ConfirmImport(as(t, access.CatalogueWrite), &inventoryv1.ConfirmImportRequest{
			GarageId: garageID, ImportType: inventoryv1.ImportType_IMPORT_TYPE_PARTS, Content: []byte("xlsx"),
		})
		assert.Equal(t, codes.InvalidArgument, status.Code(err), "nothing to import is not a successful import")
	})
}
