package handler

import (
	"context"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	"google.golang.org/genproto/googleapis/api/httpbody"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// PrintQuotePdf prints the quote (FR-DOC-05).
func (s *Server) PrintQuotePdf(ctx context.Context, req *documentv1.PrintQuotePdfRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.QuotePDF(ctx, req.GetGarageId(), req.GetQuoteId(), true)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// PrintRepairOrderPdf prints the repair order (FR-DOC-05).
func (s *Server) PrintRepairOrderPdf(ctx context.Context, req *documentv1.PrintRepairOrderPdfRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.RepairOrderPDF(ctx, req.GetGarageId(), req.GetRepairOrderId(), true)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// PrintSettlementReceiptPdf prints the settlement receipt (FR-DOC-05).
func (s *Server) PrintSettlementReceiptPdf(
	ctx context.Context, req *documentv1.PrintSettlementReceiptPdfRequest,
) (*httpbody.HttpBody, error) {
	data, err := s.uc.SettlementReceiptPDF(ctx, req.GetGarageId(), req.GetSettlementId(), true)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// PrintPaymentVoucherPdf prints one payment voucher (FR-DOC-05).
func (s *Server) PrintPaymentVoucherPdf(ctx context.Context, req *documentv1.PrintPaymentVoucherPdfRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.PaymentVoucherPDF(ctx, req.GetGarageId(), req.GetSettlementId(), req.GetPaymentId(), true)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// PrintGoodsReceiptPdf prints the goods receipt (FR-DOC-05).
func (s *Server) PrintGoodsReceiptPdf(ctx context.Context, req *documentv1.PrintGoodsReceiptPdfRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.GoodsReceiptPDF(ctx, req.GetGarageId(), req.GetGoodsReceiptId(), true)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// PrintStockIssuePdf prints the repair order's stock issue (FR-DOC-05).
func (s *Server) PrintStockIssuePdf(ctx context.Context, req *documentv1.PrintStockIssuePdfRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.StockIssuePDF(ctx, req.GetGarageId(), req.GetRepairOrderId(), true)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return pdfBody(data), nil
}

// GetPrintSettings returns the garage's print setting of every document type.
func (s *Server) GetPrintSettings(ctx context.Context, req *documentv1.GetPrintSettingsRequest) (*documentv1.GetPrintSettingsResponse, error) {
	infos, err := s.uc.PrintSettings(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	settings := make([]*documentv1.PrintSetting, 0, len(infos))
	for _, info := range infos {
		settings = append(settings, toProtoSetting(info))
	}

	return &documentv1.GetPrintSettingsResponse{Settings: settings}, nil
}

// UpdatePrintSetting sets the paper size and the copy mark of a document type.
func (s *Server) UpdatePrintSetting(ctx context.Context, req *documentv1.UpdatePrintSettingRequest) (*documentv1.PrintSetting, error) {
	t, ok := documentTypeFromProto[req.GetDocumentType()]
	if !ok {
		return nil, apperror.ToGRPCError(apperror.Validation("documentType", apperror.IssueInvalid, "document type is invalid"))
	}

	paper, ok := paperFromProto[req.GetPaperSize()]
	if !ok {
		return nil, apperror.ToGRPCError(apperror.Validation("paperSize", apperror.IssueInvalid, "paper size is invalid"))
	}

	info, err := s.uc.UpdatePrintSetting(ctx, req.GetGarageId(), t, paper, req.GetCopyMark())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return toProtoSetting(info), nil
}

var documentTypeFromProto = map[documentv1.DocumentType]printmodel.DocumentType{
	documentv1.DocumentType_DOCUMENT_TYPE_QUOTE:           printmodel.TypeQuote,
	documentv1.DocumentType_DOCUMENT_TYPE_REPAIR_ORDER:    printmodel.TypeRepairOrder,
	documentv1.DocumentType_DOCUMENT_TYPE_SETTLEMENT:      printmodel.TypeSettlement,
	documentv1.DocumentType_DOCUMENT_TYPE_PAYMENT_VOUCHER: printmodel.TypePaymentVoucher,
	documentv1.DocumentType_DOCUMENT_TYPE_GOODS_RECEIPT:   printmodel.TypeGoodsReceipt,
	documentv1.DocumentType_DOCUMENT_TYPE_STOCK_ISSUE:     printmodel.TypeStockIssue,
}

var paperFromProto = map[documentv1.PaperSize]printmodel.Paper{
	documentv1.PaperSize_PAPER_SIZE_A4: printmodel.A4,
	documentv1.PaperSize_PAPER_SIZE_A5: printmodel.A5,
}

func toProtoSetting(info usecase.PrintSettingInfo) *documentv1.PrintSetting {
	setting := &documentv1.PrintSetting{
		DocumentType: documentTypeToProto(info.Type), PaperSize: paperToProto(info.Paper), CopyMark: info.CopyMark,
	}

	for _, available := range info.Available {
		setting.AvailablePaperSizes = append(setting.AvailablePaperSizes, paperToProto(available))
	}

	return setting
}

func documentTypeToProto(t printmodel.DocumentType) documentv1.DocumentType {
	for proto, known := range documentTypeFromProto {
		if known == t {
			return proto
		}
	}

	return documentv1.DocumentType_DOCUMENT_TYPE_UNSPECIFIED
}

func paperToProto(p printmodel.Paper) documentv1.PaperSize {
	if p == printmodel.A5 {
		return documentv1.PaperSize_PAPER_SIZE_A5
	}

	return documentv1.PaperSize_PAPER_SIZE_A4
}
