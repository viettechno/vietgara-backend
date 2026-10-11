package handler

import (
	"context"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	"google.golang.org/genproto/googleapis/api/httpbody"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ExportQuotePdf renders the quote as a PDF (FR-DOC-01).
func (s *Server) ExportQuotePdf(ctx context.Context, req *documentv1.ExportQuotePdfRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.QuotePDF(ctx, req.GetGarageId(), req.GetQuoteId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// ExportRepairOrderPdf renders the repair order as a PDF (FR-DOC-01).
func (s *Server) ExportRepairOrderPdf(
	ctx context.Context, req *documentv1.ExportRepairOrderPdfRequest,
) (*httpbody.HttpBody, error) {
	data, err := s.uc.RepairOrderPDF(ctx, req.GetGarageId(), req.GetRepairOrderId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// ExportSettlementReceiptPdf renders the settlement as a receipt PDF
// (FR-DOC-01).
func (s *Server) ExportSettlementReceiptPdf(
	ctx context.Context, req *documentv1.ExportSettlementReceiptPdfRequest,
) (*httpbody.HttpBody, error) {
	data, err := s.uc.SettlementReceiptPDF(ctx, req.GetGarageId(), req.GetSettlementId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// ExportPaymentVoucherPdf renders one payment voucher in the 01-TT layout
// (FR-BIL-05).
func (s *Server) ExportPaymentVoucherPdf(
	ctx context.Context, req *documentv1.ExportPaymentVoucherPdfRequest,
) (*httpbody.HttpBody, error) {
	data, err := s.uc.PaymentVoucherPDF(ctx, req.GetGarageId(), req.GetSettlementId(), req.GetPaymentId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// ExportStockMovementsExcel renders the stock movement report as an
// Excel workbook (FR-DOC-02).
func (s *Server) ExportStockMovementsExcel(
	ctx context.Context, req *documentv1.ExportStockMovementsExcelRequest,
) (*httpbody.HttpBody, error) {
	data, err := s.uc.StockMovementsExcel(ctx, req.GetGarageId(), req.GetItemId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return excelBody(data), nil
}
