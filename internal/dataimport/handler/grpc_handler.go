// Package handler exposes the Data Import service (FR-INV-14) as gRPC and,
// through grpc-gateway, as REST. Authorization for the RPC itself is "any
// active member": which permission an import needs depends on its type and on
// the columns the file uses, and is enforced inside the use case with a row-0
// PERMISSION_REQUIRED error.
package handler

import (
	"context"
	"math"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	"google.golang.org/genproto/googleapis/api/httpbody"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// ContentType is the MIME type of an .xlsx workbook.
const ContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// Server implements inventoryv1.ImportServiceServer.
type Server struct {
	inventoryv1.UnimplementedImportServiceServer

	imports *usecase.UseCase
}

// NewServer wires the handler to its use case.
func NewServer(imports *usecase.UseCase) *Server {
	return &Server{imports: imports}
}

// Policies declares what each RPC requires; the use case checks the
// permissions of the import type itself.
func Policies() middleware.Policies {
	member := access.Member()

	return middleware.Policies{
		inventoryv1.ImportService_DownloadImportTemplate_FullMethodName: middleware.GarageMethod(member),
		inventoryv1.ImportService_PreviewImport_FullMethodName:          middleware.GarageMethod(member),
		inventoryv1.ImportService_ConfirmImport_FullMethodName:          middleware.GarageMethod(member),
	}
}

// DownloadImportTemplate streams the template workbook of a type.
func (s *Server) DownloadImportTemplate(
	_ context.Context, req *inventoryv1.DownloadImportTemplateRequest,
) (*httpbody.HttpBody, error) {
	typ, ok := importType(req.GetImportType())
	if !ok {
		return nil, usecase.ToGRPCError(domain.ErrUnsupportedType)
	}

	data, _, err := s.imports.DownloadTemplate(typ)
	if err != nil {
		return nil, usecase.ToGRPCError(err)
	}

	return &httpbody.HttpBody{ContentType: ContentType, Data: data}, nil
}

// PreviewImport checks a workbook without writing anything.
func (s *Server) PreviewImport(
	ctx context.Context, req *inventoryv1.PreviewImportRequest,
) (*inventoryv1.PreviewImportResponse, error) {
	in, err := importRequest(ctx, req.GetGarageId(), req.GetImportType(), req.GetFileName(), req.GetContent())
	if err != nil {
		return nil, err
	}

	preview, err := s.imports.Preview(ctx, in)
	if err != nil {
		return nil, usecase.ToGRPCError(err)
	}

	return &inventoryv1.PreviewImportResponse{Preview: previewToProto(preview)}, nil
}

// ConfirmImport imports a workbook as a whole.
func (s *Server) ConfirmImport(
	ctx context.Context, req *inventoryv1.ConfirmImportRequest,
) (*inventoryv1.ConfirmImportResponse, error) {
	in, err := importRequest(ctx, req.GetGarageId(), req.GetImportType(), req.GetFileName(), req.GetContent())
	if err != nil {
		return nil, err
	}

	result, err := s.imports.Confirm(ctx, in)
	if err != nil {
		return nil, usecase.ToGRPCError(err)
	}

	return &inventoryv1.ConfirmImportResponse{Result: resultToProto(result)}, nil
}

// importRequest builds the use case request of a preview or a confirmation
// from the caller and the uploaded file.
func importRequest(
	ctx context.Context, garageID string, t inventoryv1.ImportType, fileName string, content []byte,
) (usecase.Request, error) {
	typ, ok := importType(t)
	if !ok {
		return usecase.Request{}, usecase.ToGRPCError(domain.ErrUnsupportedType)
	}

	return usecase.Request{
		GarageID: garageID, ActorID: middleware.UserID(ctx), Type: typ,
		FileName: fileName, Content: content, Permissions: permissions(ctx),
	}, nil
}

func permissions(ctx context.Context) usecase.Permissions {
	a, ok := access.FromContext(ctx)
	if !ok {
		return usecase.Permissions{}
	}

	return usecase.Permissions{
		CatalogueWrite: a.Has(access.CatalogueWrite), InventoryWrite: a.Has(access.InventoryWrite),
		InventoryCostRead: a.Has(access.InventoryCostRead), InventoryCount: a.Has(access.InventoryCount),
	}
}

func importType(t inventoryv1.ImportType) (domain.Type, bool) {
	switch t {
	case inventoryv1.ImportType_IMPORT_TYPE_PARTS:
		return domain.TypeParts, true
	case inventoryv1.ImportType_IMPORT_TYPE_GOODS_RECEIPTS:
		return domain.TypeGoodsReceipts, true
	case inventoryv1.ImportType_IMPORT_TYPE_STOCK_COUNTS:
		return domain.TypeStockCounts, true
	case inventoryv1.ImportType_IMPORT_TYPE_UNSPECIFIED:
		return "", false
	default:
		return "", false
	}
}

func importTypeToProto(t domain.Type) inventoryv1.ImportType {
	switch t {
	case domain.TypeParts:
		return inventoryv1.ImportType_IMPORT_TYPE_PARTS
	case domain.TypeGoodsReceipts:
		return inventoryv1.ImportType_IMPORT_TYPE_GOODS_RECEIPTS
	case domain.TypeStockCounts:
		return inventoryv1.ImportType_IMPORT_TYPE_STOCK_COUNTS
	default:
		return inventoryv1.ImportType_IMPORT_TYPE_UNSPECIFIED
	}
}

func previewToProto(p *domain.Preview) *inventoryv1.ImportPreview {
	out := &inventoryv1.ImportPreview{
		ImportType: importTypeToProto(p.Type), TotalRows: clamp(p.TotalRows), ValidRows: clamp(p.ValidRows),
		Errors: make([]*inventoryv1.ImportRowError, 0, len(p.Errors)), ErrorsTruncated: p.ErrorsTruncated,
		Columns: p.Columns, SampleRows: make([]*inventoryv1.ImportPreviewRow, 0, len(p.SampleRows)),
		Counts: countMap(p.Counts), CanConfirm: p.CanConfirm,
	}

	for _, e := range p.Errors {
		out.Errors = append(out.Errors, &inventoryv1.ImportRowError{
			Row: clamp(e.Row), Column: e.Column, Code: e.Code, Message: e.Message,
		})
	}

	for _, row := range p.SampleRows {
		out.SampleRows = append(out.SampleRows, &inventoryv1.ImportPreviewRow{Row: clamp(row.Row), Cells: row.Cells})
	}

	return out
}

func resultToProto(r *domain.Result) *inventoryv1.ImportResult {
	return &inventoryv1.ImportResult{
		ImportType: importTypeToProto(r.Type), RowsImported: clamp(r.RowsImported), Counts: countMap(r.Counts),
		DocumentId: r.DocumentID, DocumentNumber: r.DocumentNumber,
	}
}

func countMap(in map[string]int) map[string]int32 {
	out := make(map[string]int32, len(in))
	for k, v := range in {
		out[k] = clamp(v)
	}

	return out
}

func clamp(n int) int32 {
	return int32(min(max(n, 0), math.MaxInt32)) //nolint:gosec // clamped
}
