// Package handler exposes the Document/Print module (FRD 3.13) as a gRPC
// service, served as REST through grpc-gateway. Each RPC returns
// google.api.HttpBody (raw PDF/Excel bytes with their content type)
// rather than JSON -- the one place in the API that isn't, since a
// document download doesn't fit a JSON envelope -- but it still runs
// through the same auth/garage middleware chain as every other RPC,
// unlike a plain HTTP handler registered outside it. No policy of its
// own: each export needs the permission of the data it renders
// (quote.read, billing.read, inventory.read), applied the same way
// every other read in that module is.
package handler

import (
	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	"google.golang.org/genproto/googleapis/api/httpbody"

	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// Server implements documentv1.DocumentServiceServer.
type Server struct {
	documentv1.UnimplementedDocumentServiceServer

	uc *usecase.DocumentUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.DocumentUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares the permission every RPC requires in its garage,
// matching the module whose data it renders.
func Policies() middleware.Policies {
	quoteRead := access.Requires(access.QuoteRead)
	repairOrderRead := access.Requires(access.RepairOrderRead)
	billingRead := access.Requires(access.BillingRead)
	inventoryRead := access.Requires(access.InventoryRead)

	return middleware.Policies{
		documentv1.DocumentService_ExportQuotePdf_FullMethodName:             middleware.GarageMethod(quoteRead),
		documentv1.DocumentService_ExportRepairOrderPdf_FullMethodName:       middleware.GarageMethod(repairOrderRead),
		documentv1.DocumentService_ExportSettlementReceiptPdf_FullMethodName: middleware.GarageMethod(billingRead),
		documentv1.DocumentService_ExportPaymentVoucherPdf_FullMethodName:    middleware.GarageMethod(billingRead),
		documentv1.DocumentService_ExportStockMovementsExcel_FullMethodName:  middleware.GarageMethod(inventoryRead),
	}
}

func pdfBody(data []byte) *httpbody.HttpBody {
	return &httpbody.HttpBody{ContentType: "application/pdf", Data: data}
}

func excelBody(data []byte) *httpbody.HttpBody {
	return &httpbody.HttpBody{
		ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Data: data,
	}
}
