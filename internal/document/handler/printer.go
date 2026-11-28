package handler

import (
	"context"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	"google.golang.org/genproto/googleapis/api/httpbody"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// The RPCs of Release 13.0: the garage's printers (FR-DOC-07), the roll
// prints (FR-DOC-06) and the part labels (FR-INV-13).

// ListPrinters returns the garage's printers.
func (s *Server) ListPrinters(ctx context.Context, req *documentv1.ListPrintersRequest) (*documentv1.ListPrintersResponse, error) {
	printers, err := s.uc.Printers(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &documentv1.ListPrintersResponse{Printers: make([]*documentv1.Printer, 0, len(printers))}
	for _, p := range printers {
		resp.Printers = append(resp.Printers, printerToProto(p))
	}

	return resp, nil
}

// CreatePrinter saves a printer.
func (s *Server) CreatePrinter(ctx context.Context, req *documentv1.CreatePrinterRequest) (*documentv1.Printer, error) {
	p, err := printerFromProto(req.GetPrinter())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	created, err := s.uc.CreatePrinter(ctx, req.GetGarageId(), p)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return printerToProto(created), nil
}

// UpdatePrinter replaces a printer.
func (s *Server) UpdatePrinter(ctx context.Context, req *documentv1.UpdatePrinterRequest) (*documentv1.Printer, error) {
	p, err := printerFromProto(req.GetPrinter())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	p.ID = req.GetPrinterId()

	updated, err := s.uc.UpdatePrinter(ctx, req.GetGarageId(), p)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return printerToProto(updated), nil
}

// DeletePrinter removes a printer.
func (s *Server) DeletePrinter(ctx context.Context, req *documentv1.DeletePrinterRequest) (*documentv1.DeletePrinterResponse, error) {
	if err := s.uc.DeletePrinter(ctx, req.GetGarageId(), req.GetPrinterId()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &documentv1.DeletePrinterResponse{}, nil
}

// PrintPrinterTestPage returns the printer's test page.
func (s *Server) PrintPrinterTestPage(ctx context.Context, req *documentv1.PrintPrinterTestPageRequest) (*httpbody.HttpBody, error) {
	data, contentType, err := s.uc.PrinterTestPage(ctx, req.GetGarageId(), req.GetPrinterId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &httpbody.HttpBody{ContentType: contentType, Data: data}, nil
}

// PrintQuoteEscPos prints the quote on a roll.
func (s *Server) PrintQuoteEscPos(ctx context.Context, req *documentv1.PrintQuoteEscPosRequest) (*httpbody.HttpBody, error) {
	data, err := s.uc.QuoteEscPos(ctx, req.GetGarageId(), req.GetQuoteId(), paperFromProtoAny[req.GetPaperSize()])
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return escposBody(data), nil
}

// PrintPaymentVoucherEscPos prints a payment voucher on a roll.
func (s *Server) PrintPaymentVoucherEscPos(
	ctx context.Context, req *documentv1.PrintPaymentVoucherEscPosRequest,
) (*httpbody.HttpBody, error) {
	data, err := s.uc.PaymentVoucherEscPos(ctx, req.GetGarageId(), req.GetSettlementId(), req.GetPaymentId(), paperFromProtoAny[req.GetPaperSize()])
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return escposBody(data), nil
}

// PrintPartLabels prints the labels of parts.
func (s *Server) PrintPartLabels(ctx context.Context, req *documentv1.PrintPartLabelsRequest) (*httpbody.HttpBody, error) {
	parts := make([]usecase.LabelCopies, 0, len(req.GetParts()))
	for _, p := range req.GetParts() {
		parts = append(parts, usecase.LabelCopies{PartID: p.GetPartId(), Copies: int(p.GetCopies())})
	}

	data, err := s.uc.PartLabels(ctx, req.GetGarageId(), paperFromProtoAny[req.GetPaperSize()], parts)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return escposBody(data), nil
}

func escposBody(data []byte) *httpbody.HttpBody {
	return &httpbody.HttpBody{ContentType: usecase.ContentTypeEscPos, Data: data}
}

// paperFromProtoAny maps every paper size, the rolls and the label included;
// an unspecified size maps to "", which the use case refuses.
var paperFromProtoAny = map[documentv1.PaperSize]printmodel.Paper{
	documentv1.PaperSize_PAPER_SIZE_A4:          printmodel.A4,
	documentv1.PaperSize_PAPER_SIZE_A5:          printmodel.A5,
	documentv1.PaperSize_PAPER_SIZE_K80:         printmodel.K80,
	documentv1.PaperSize_PAPER_SIZE_K57:         printmodel.K57,
	documentv1.PaperSize_PAPER_SIZE_LABEL_50X30: printmodel.Label5030,
}

var paperToProtoAny = func() map[printmodel.Paper]documentv1.PaperSize {
	out := make(map[printmodel.Paper]documentv1.PaperSize, len(paperFromProtoAny))
	for proto, p := range paperFromProtoAny {
		out[p] = proto
	}

	return out
}()

var protocolFromProto = map[documentv1.PrinterProtocol]usecase.PrinterProtocol{
	documentv1.PrinterProtocol_PRINTER_PROTOCOL_RAW: usecase.ProtocolRaw,
	documentv1.PrinterProtocol_PRINTER_PROTOCOL_IPP: usecase.ProtocolIPP,
}

func printerFromProto(p *documentv1.Printer) (usecase.Printer, error) {
	if _, known := paperFromProtoAny[p.GetPaperSize()]; !known && p.GetPaperSize() != documentv1.PaperSize_PAPER_SIZE_UNSPECIFIED {
		return usecase.Printer{}, apperror.Validation("paperSize", apperror.IssueInvalid, "paper size is invalid")
	}

	out := usecase.Printer{
		Name: p.GetName(), Protocol: protocolFromProto[p.GetProtocol()], Host: p.GetHost(), Port: int(p.GetPort()),
		Path: p.GetPath(), Paper: paperFromProtoAny[p.GetPaperSize()],
	}

	for _, t := range p.GetDocumentTypes() {
		mapped, ok := printerTypeFromProto(t)
		if !ok {
			return usecase.Printer{}, apperror.Validation("documentTypes", apperror.IssueInvalid, "document type is invalid")
		}

		out.DocumentTypes = append(out.DocumentTypes, mapped)
	}

	return out, nil
}

func printerTypeFromProto(t documentv1.DocumentType) (printmodel.DocumentType, bool) {
	if t == documentv1.DocumentType_DOCUMENT_TYPE_PART_LABEL {
		return printmodel.TypePartLabel, true
	}

	mapped, ok := documentTypeFromProto[t]

	return mapped, ok
}

func printerToProto(p usecase.Printer) *documentv1.Printer {
	out := &documentv1.Printer{
		Id: p.ID, Name: p.Name, Host: p.Host, Port: int32(p.Port), //nolint:gosec // at most 65535, checked on save
		Path: p.Path, PaperSize: paperToProtoAny[p.Paper],
		CreatedAt: timestamppb.New(p.CreatedAt), UpdatedAt: timestamppb.New(p.UpdatedAt),
	}

	switch p.Protocol {
	case usecase.ProtocolRaw:
		out.Protocol = documentv1.PrinterProtocol_PRINTER_PROTOCOL_RAW
	case usecase.ProtocolIPP:
		out.Protocol = documentv1.PrinterProtocol_PRINTER_PROTOCOL_IPP
	}

	for _, t := range p.DocumentTypes {
		if t == printmodel.TypePartLabel {
			out.DocumentTypes = append(out.DocumentTypes, documentv1.DocumentType_DOCUMENT_TYPE_PART_LABEL)

			continue
		}

		out.DocumentTypes = append(out.DocumentTypes, documentTypeToProto[t])
	}

	return out
}
