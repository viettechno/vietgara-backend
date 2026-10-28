package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/render"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

// errTemplateGone means the registry lost a template that a print log row
// names: a released version was removed from the code.
var errTemplateGone = errors.New("document: no template for the document type, paper size and version")

// PrintSetting is a garage's choice for one document type (FR-DOC-04).
type PrintSetting struct {
	Paper    printmodel.Paper
	CopyMark bool
}

// DefaultPrintSetting is what a garage that never chose prints: A4, and no
// copy mark.
var DefaultPrintSetting = PrintSetting{Paper: printmodel.A4}

// PrintState is what the print log says about one document on one paper size.
type PrintState struct {
	// FirstVersion is the template version of the first print on that paper
	// size, or 0 when the document was never printed on it.
	FirstVersion int
	// Prints is the number of times the document was printed, on any paper.
	Prints int
}

// PrintStore keeps the print settings and the print log; implemented by
// repository.PostgresPrintStore. Every call runs in the transaction of the
// request, bound to its garage.
type PrintStore interface {
	// Settings returns the garage's settings; a type it never changed is
	// absent.
	Settings(ctx context.Context, garageID string) (map[printmodel.DocumentType]PrintSetting, error)
	SaveSetting(ctx context.Context, garageID string, t printmodel.DocumentType, s PrintSetting) error
	State(ctx context.Context, garageID string, t printmodel.DocumentType, documentID string, paper printmodel.Paper) (PrintState, error)
	// Record adds one row to the print log.
	Record(ctx context.Context, garageID string, t printmodel.DocumentType, documentID string, paper printmodel.Paper, version int, copyMark bool) error
}

// PrintSettingInfo is a setting with the sizes its document type offers.
type PrintSettingInfo struct {
	Type      printmodel.DocumentType
	Paper     printmodel.Paper
	CopyMark  bool
	Available []printmodel.Paper
}

// PrintSettings returns the garage's setting for every printable document,
// defaults filled in.
func (uc *DocumentUseCase) PrintSettings(ctx context.Context, garageID string) ([]PrintSettingInfo, error) {
	chosen, err := uc.Prints.Settings(ctx, garageID)
	if err != nil {
		return nil, apperror.Internal("read print settings", err)
	}

	infos := make([]PrintSettingInfo, 0, len(printmodel.Types()))

	for _, t := range printmodel.Types() {
		setting, ok := chosen[t]
		if !ok {
			setting = DefaultPrintSetting
		}

		infos = append(infos, PrintSettingInfo{Type: t, Paper: setting.Paper, CopyMark: setting.CopyMark, Available: render.Papers(t)})
	}

	return infos, nil
}

// UpdatePrintSetting sets the paper size and the copy mark of a document
// type. A size the type has no template for is refused.
func (uc *DocumentUseCase) UpdatePrintSetting(
	ctx context.Context, garageID string, t printmodel.DocumentType, paper printmodel.Paper, copyMark bool,
) (PrintSettingInfo, error) {
	if !t.Valid() {
		return PrintSettingInfo{}, apperror.Validation("documentType", apperror.IssueInvalid, "document type is invalid")
	}

	if _, ok := render.Latest(t, paper); !ok {
		return PrintSettingInfo{}, apperror.Validation("paperSize", apperror.IssueInvalid, "this document cannot be printed on that paper size")
	}

	if err := uc.Prints.SaveSetting(ctx, garageID, t, PrintSetting{Paper: paper, CopyMark: copyMark}); err != nil {
		return PrintSettingInfo{}, apperror.Internal("save print setting", err)
	}

	return PrintSettingInfo{Type: t, Paper: paper, CopyMark: copyMark, Available: render.Papers(t)}, nil
}

// pdf renders model m of document documentID the way the garage prints it
// (FR-DOC-04): on the paper size it chose, with the template version of the
// document's first print, so a reprint matches it. When printed is set the
// render is a print (FR-DOC-05): a reprint carries the copy mark if the garage
// wants one, and the print is logged. An export is not logged.
func (uc *DocumentUseCase) pdf(ctx context.Context, garageID, documentID string, m printmodel.Model, printed bool) ([]byte, error) {
	t := m.Type()

	chosen, err := uc.Prints.Settings(ctx, garageID)
	if err != nil {
		return nil, apperror.Internal("read print settings", err)
	}

	setting, ok := chosen[t]
	if !ok {
		setting = DefaultPrintSetting
	}

	if _, offered := render.Latest(t, setting.Paper); !offered {
		setting.Paper = printmodel.A4
	}

	state, err := uc.Prints.State(ctx, garageID, t, documentID, setting.Paper)
	if err != nil {
		return nil, apperror.Internal("read print state", err)
	}

	tpl, err := templateFor(t, setting.Paper, state.FirstVersion)
	if err != nil {
		return nil, err
	}

	copyMark := printed && setting.CopyMark && state.Prints > 0

	data, err := tpl.Render(m, copyMark)
	if err != nil {
		return nil, apperror.Internal("render "+string(t)+" pdf", err)
	}

	if printed {
		if err := uc.Prints.Record(ctx, garageID, t, documentID, setting.Paper, tpl.Version, copyMark); err != nil {
			return nil, apperror.Internal("log print", err)
		}
	}

	return data, nil
}

// templateFor returns the template a document is printed with: the version
// of its first print, or the newest one when it was never printed.
func templateFor(t printmodel.DocumentType, paper printmodel.Paper, firstVersion int) (render.Template, error) {
	if firstVersion > 0 {
		if tpl, ok := render.Lookup(t, paper, firstVersion); ok {
			return tpl, nil
		}

		return render.Template{}, apperror.Internal("find template", errTemplateGone)
	}

	tpl, ok := render.Latest(t, paper)
	if !ok {
		return render.Template{}, apperror.Internal("find template", errTemplateGone)
	}

	return tpl, nil
}

// QuotePDF renders a quote as a PDF (FR-DOC-01), or prints it (FR-DOC-05).
//
//nolint:dupl // each export reads one kind of document and builds its own print model
func (uc *DocumentUseCase) QuotePDF(ctx context.Context, garageID, quoteID string, printed bool) ([]byte, error) {
	quote, err := uc.Quotes.Get(ctx, garageID, quoteID)
	if err != nil {
		return nil, toAppError("export quote pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, quote.Issuer)
	if err != nil {
		return nil, err
	}

	return uc.pdf(ctx, garageID, quoteID, printmodel.Quote{
		Garage: garage, BillTo: billTo(quote.BillingParty), Number: quote.Number, Status: quote.Status, CustomerName: quote.CustomerName,
		VehiclePlate: quote.VehiclePlate, Notes: quote.Notes, Lines: quote.Lines, TotalAmount: quote.TotalAmount,
	}, printed)
}

// RepairOrderPDF renders a repair order as a PDF (FR-DOC-01), or prints it.
func (uc *DocumentUseCase) RepairOrderPDF(ctx context.Context, garageID, repairOrderID string, printed bool) ([]byte, error) {
	ro, err := uc.RepairOrders.Get(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError("export repair order pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, ro.Issuer)
	if err != nil {
		return nil, err
	}

	return uc.pdf(ctx, garageID, repairOrderID, printmodel.RepairOrder{
		Garage: garage, BillTo: billTo(ro.BillingParty), Number: ro.Number, Status: ro.Status, CustomerName: ro.CustomerName,
		VehiclePlate: ro.VehiclePlate, Lines: ro.Lines, TotalAmount: ro.TotalAmount,
	}, printed)
}

// SettlementReceiptPDF renders a settlement as a receipt PDF (FR-DOC-01), or
// prints it.
//
//nolint:dupl // each export reads one kind of document and builds its own print model
func (uc *DocumentUseCase) SettlementReceiptPDF(ctx context.Context, garageID, settlementID string, printed bool) ([]byte, error) {
	settlement, err := uc.Settlements.Get(ctx, garageID, settlementID)
	if err != nil {
		return nil, toAppError("export settlement receipt pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, settlement.Issuer)
	if err != nil {
		return nil, err
	}

	return uc.pdf(ctx, garageID, settlementID, printmodel.Settlement{
		Garage: garage, BillTo: billTo(settlement.BillingParty), Number: settlement.Number, Status: settlement.Status,
		CustomerName: settlement.CustomerName, VehiclePlate: settlement.VehiclePlate, TotalAmount: settlement.TotalAmount,
		PaidAmount: settlement.PaidAmount, Payments: settlement.Payments,
	}, printed)
}

// PaymentVoucherPDF renders one payment voucher as a PDF in the 01-TT layout
// (FR-BIL-05), from the legal identity it was issued with, or prints it. The
// voucher's document id is the payment's.
func (uc *DocumentUseCase) PaymentVoucherPDF(ctx context.Context, garageID, settlementID, paymentID string, printed bool) ([]byte, error) {
	settlement, err := uc.Settlements.Get(ctx, garageID, settlementID)
	if err != nil {
		return nil, toAppError("export payment voucher pdf", err)
	}

	voucher, ok := findVoucher(settlement.Vouchers, paymentID)
	if !ok {
		return nil, apperror.New(apperror.CodeNotFound, "payment not found")
	}

	garage, err := uc.letterhead(ctx, garageID, voucher.Issuer)
	if err != nil {
		return nil, err
	}

	return uc.pdf(ctx, garageID, paymentID, printmodel.PaymentVoucher{
		Garage: garage, Number: voucher.Number, Date: voucher.Date, PayerName: voucher.PayerName,
		PayerAddress: voucher.PayerAddress, Reason: voucher.Reason, Amount: voucher.Amount, Method: voucher.Method,
		SettlementNumber: settlement.Number, CollectorName: voucher.CollectorName,
		Voided: voucher.Voided, VoidReason: voucher.VoidReason,
	}, printed)
}

// GoodsReceiptPDF renders a goods receipt as a PDF (FR-INV-06), or prints it.
func (uc *DocumentUseCase) GoodsReceiptPDF(ctx context.Context, garageID, receiptID string, printed bool) ([]byte, error) {
	receipt, err := uc.Inventory.GoodsReceipt(ctx, garageID, receiptID)
	if err != nil {
		return nil, toAppError("export goods receipt pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, issuer.Issuer{})
	if err != nil {
		return nil, err
	}

	return uc.pdf(ctx, garageID, receiptID, printmodel.GoodsReceipt{
		Garage: garage, Number: receipt.Number, Status: receipt.Status, Date: receipt.Date,
		DeliveryNoteNumber: receipt.DeliveryNoteNumber, Notes: receipt.Notes, Lines: receipt.Lines,
		ShowCost: receipt.ShowCost, TotalAmount: receipt.TotalAmount,
	}, printed)
}

// StockIssuePDF renders a repair order's stock issue (phiếu xuất kho) as a
// PDF (FR-INV-20), or prints it. The document id is the repair order's.
func (uc *DocumentUseCase) StockIssuePDF(ctx context.Context, garageID, repairOrderID string, printed bool) ([]byte, error) {
	issue, err := uc.Inventory.StockIssue(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError("export stock issue pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, issuer.Issuer{})
	if err != nil {
		return nil, err
	}

	return uc.pdf(ctx, garageID, repairOrderID, printmodel.StockIssue{
		Garage: garage, Number: issue.Number, Status: issue.Status, RepairOrderNumber: issue.RepairOrderNumber,
		PostedAt: issue.PostedAt, PostedBy: issue.PostedBy, Lines: issue.Lines,
	}, printed)
}
