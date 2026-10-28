package handler

import (
	"context"
	"time"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"
	"google.golang.org/genproto/googleapis/api/httpbody"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ExportQuotePdf renders the quote as a PDF (FR-DOC-01).
func (s *Server) ExportQuotePdf(ctx context.Context, req *documentv1.ExportQuotePdfRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.QuotePDF(ctx, req.GetGarageId(), req.GetQuoteId(), false)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// ExportRepairOrderPdf renders the repair order as a PDF (FR-DOC-01).
func (s *Server) ExportRepairOrderPdf(
	ctx context.Context, req *documentv1.ExportRepairOrderPdfRequest,
) (*httpbody.HttpBody, error) {
	data, err := s.uc.RepairOrderPDF(ctx, req.GetGarageId(), req.GetRepairOrderId(), false)
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
	data, err := s.uc.SettlementReceiptPDF(ctx, req.GetGarageId(), req.GetSettlementId(), false)
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
	data, err := s.uc.PaymentVoucherPDF(ctx, req.GetGarageId(), req.GetSettlementId(), req.GetPaymentId(), false)
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
	data, err := s.uc.StockMovementsExcel(ctx, req.GetGarageId(), req.GetPartId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return excelBody(data), nil
}

// ExportRevenueExcel renders the revenue-by-period report as an Excel
// workbook (FR-DOC-02).
func (s *Server) ExportRevenueExcel(ctx context.Context, req *documentv1.ExportRevenueExcelRequest) (*httpbody.HttpBody, error) {
	from, err := time.Parse(dateLayout, req.GetFrom())
	if err != nil {
		return nil, apperror.ToGRPCError(apperror.Validation("from", apperror.IssueInvalid, "from must be a valid date (YYYY-MM-DD)"))
	}

	to, err := time.Parse(dateLayout, req.GetTo())
	if err != nil {
		return nil, apperror.ToGRPCError(apperror.Validation("to", apperror.IssueInvalid, "to must be a valid date (YYYY-MM-DD)"))
	}

	data, err := s.uc.RevenueExcel(ctx, req.GetGarageId(), revenuePeriod(req.GetPeriod()), from, to)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return excelBody(data), nil
}

const dateLayout = "2006-01-02"

// revenuePeriod names a period for the use case; an unspecified one is
// refused by the Reporting module.
func revenuePeriod(p reportingv1.RevenuePeriod) string {
	switch p {
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_DAY:
		return "day"
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_MONTH:
		return "month"
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_QUARTER:
		return "quarter"
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_YEAR:
		return "year"
	case reportingv1.RevenuePeriod_REVENUE_PERIOD_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

// ExportGoodsReceiptPdf renders a goods receipt as a PDF (FR-INV-06).
func (s *Server) ExportGoodsReceiptPdf(ctx context.Context, req *documentv1.ExportGoodsReceiptPdfRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.GoodsReceiptPDF(ctx, req.GetGarageId(), req.GetGoodsReceiptId(), false)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// ExportStockIssuePdf renders a repair order's stock issue as a PDF
// (FR-INV-20).
func (s *Server) ExportStockIssuePdf(ctx context.Context, req *documentv1.ExportStockIssuePdfRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.StockIssuePDF(ctx, req.GetGarageId(), req.GetRepairOrderId(), false)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// ExportStockCountExcel renders a stock count as an Excel workbook (FR-INV-12).
func (s *Server) ExportStockCountExcel(ctx context.Context, req *documentv1.ExportStockCountExcelRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.StockCountExcel(ctx, req.GetGarageId(), req.GetStockCountId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return excelBody(data), nil
}

// ExportPurchasesExcel renders the purchases-by-supplier report as an Excel
// workbook (FR-INV-19).
func (s *Server) ExportPurchasesExcel(ctx context.Context, req *documentv1.ExportPurchasesExcelRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.PurchasesExcel(ctx, req.GetGarageId(), req.GetSupplierId(), req.GetFrom(), req.GetTo())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return excelBody(data), nil
}

// ExportStockValuationExcel renders the stock valuation report as an Excel
// workbook (FR-INV-07).
func (s *Server) ExportStockValuationExcel(ctx context.Context, req *documentv1.ExportStockValuationExcelRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.StockValuationExcel(ctx, req.GetGarageId(), req.GetAt())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return excelBody(data), nil
}
