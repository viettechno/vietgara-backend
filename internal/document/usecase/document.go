// Package usecase renders the documents of the Document/Print module
// (FRD 3.13): a quote PDF, a settlement receipt PDF (FR-DOC-01), the
// goods receipt and stock issue PDFs and the inventory reports as Excel
// workbooks (FR-INV-06/07/19/20), and the stock movement report as an Excel
// workbook (FR-DOC-02). It owns no
// tables of its own -- every document is generated fresh from the owning
// module's own data, read through narrow reader interfaces implemented
// by adapters in cmd/server, the same cross-module decoupling pattern
// used everywhere else (HLD Section 2).
package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/viettechno/vietgara-backend/internal/document/render"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

// QuoteInfo is the Quote & Pricing module's view of a quote that
// Document/Print needs to render its PDF.
type QuoteInfo struct {
	Number, Status, CustomerName, VehiclePlate, Notes string
	Lines                                             []render.DocumentLine
	TotalAmount                                       int64
	// Issuer is the garage's legal identity when the quote was issued
	// (FR-TEN-06).
	Issuer issuer.Issuer
	// BillingParty is who the quote is billed to (FR-CUS-07).
	BillingParty billingparty.Party
}

// QuoteReader reads a quote for PDF export; implemented by an adapter
// over the Quote & Pricing module in cmd/server. Returns an
// *apperror.AppError directly (e.g. apperror.NotFound) rather than a
// module-specific sentinel -- this module has no domain errors of its
// own to translate into, unlike a module with its own business rules.
type QuoteReader interface {
	Get(ctx context.Context, garageID, quoteID string) (QuoteInfo, error)
}

// RepairOrderInfo is the Repair Order module's view of a repair order
// that Document/Print needs to render its PDF.
type RepairOrderInfo struct {
	Number, Status, CustomerName, VehiclePlate string
	Lines                                      []render.DocumentLine
	TotalAmount                                int64
	// Issuer is the garage's legal identity when the repair order was
	// created (FR-TEN-06).
	Issuer issuer.Issuer
	// BillingParty is who the repair order is billed to (FR-CUS-07).
	BillingParty billingparty.Party
}

// RepairOrderReader reads a repair order for PDF export; implemented by
// an adapter over the Repair Order module in cmd/server.
type RepairOrderReader interface {
	Get(ctx context.Context, garageID, repairOrderID string) (RepairOrderInfo, error)
}

// SettlementInfo is the Billing & Settlement module's view of a
// settlement that Document/Print needs to render its receipt PDF.
type SettlementInfo struct {
	Number, Status, CustomerName, VehiclePlate string
	TotalAmount, PaidAmount                    int64
	// Payments are the payments that count, for the receipt.
	Payments []render.PaymentRow
	// Vouchers are every payment voucher of the settlement, voided ones
	// included.
	Vouchers []VoucherInfo
	// Issuer is the garage's legal identity when the settlement was
	// created (FR-TEN-06).
	Issuer issuer.Issuer
	// BillingParty is who the settlement is billed to (FR-CUS-07).
	BillingParty billingparty.Party
}

// VoucherInfo is one payment voucher (phiếu thu) of a settlement.
type VoucherInfo struct {
	ID, Number, Date, PayerName, PayerAddress, Reason, Method, CollectorName string
	Amount                                                                   int64
	Voided                                                                   bool
	VoidReason                                                               string
	// Issuer is the garage's legal identity when the voucher was issued
	// (FR-TEN-06).
	Issuer issuer.Issuer
}

// SettlementReader reads a settlement for receipt PDF export; implemented
// by an adapter over the Billing & Settlement module in cmd/server.
type SettlementReader interface {
	Get(ctx context.Context, garageID, settlementID string) (SettlementInfo, error)
}

// GoodsReceiptInfo is the Inventory module's view of a goods receipt that
// Document/Print needs to render its PDF.
type GoodsReceiptInfo struct {
	Number, Status, Date, DeliveryNoteNumber, Notes string
	Lines                                           []render.GoodsReceiptRow
	// ShowCost is false for a caller without inventory.cost.read: the
	// document then prints quantities only (FR-INV-08).
	ShowCost    bool
	TotalAmount int64
}

// StockIssueInfo is the Inventory module's view of a stock issue that
// Document/Print needs to render its PDF.
type StockIssueInfo struct {
	Number, Status, RepairOrderNumber, PostedAt, PostedBy string
	Lines                                                 []render.StockIssueRow
}

// InventoryDocumentReader reads the Release 7.0 inventory documents and
// reports; implemented by an adapter over the Inventory module in
// cmd/server. The two reports need inventory.cost.read, which the RPC
// policy checks.
type InventoryDocumentReader interface {
	GoodsReceipt(ctx context.Context, garageID, receiptID string) (GoodsReceiptInfo, error)
	// StockIssue returns the stock issue of a repair order.
	StockIssue(ctx context.Context, garageID, repairOrderID string) (StockIssueInfo, error)
	// Purchases lists the purchases report; from is inclusive and to
	// exclusive, "YYYY-MM-DD" or empty.
	Purchases(ctx context.Context, garageID, supplierID, from, to string) ([]render.PurchaseRow, int64, error)
	// Valuation lists the stock valuation at a date ("YYYY-MM-DD", empty for
	// today).
	Valuation(ctx context.Context, garageID, at string) ([]render.ValuationRow, int64, error)
}

// StockMovementReader reads the stock movement report for Excel export;
// implemented by an adapter over the Inventory module in cmd/server.
type StockMovementReader interface {
	List(ctx context.Context, garageID, itemID string) ([]render.StockMovementRow, error)
}

// RevenueInfo is a garage's revenue bucketed by period, as the Reporting
// module reports it.
type RevenueInfo struct {
	Points      []render.RevenueRow
	TotalAmount int64
}

// RevenueReader reads the revenue report for Excel export; implemented by an
// adapter over the Reporting module in cmd/server. period is "day", "month",
// "quarter" or "year"; from is inclusive, to exclusive.
type RevenueReader interface {
	Revenue(ctx context.Context, garageID, period string, from, to time.Time) (RevenueInfo, error)
}

// GarageHeaderInfo is the Tenant module's view of a garage that every
// document's letterhead needs.
type GarageHeaderInfo struct {
	// Name and Address are the legal name and address, or the trading ones
	// when the garage has no legal profile (FR-TEN-06).
	Name, Address, Phone string
	TaxCode              string
	Representative       string
	Bank                 string
	// Logo is the logo's image content, empty when the garage has none.
	Logo []byte
}

// GarageReader reads a garage's letterhead; implemented by an adapter
// over the Tenant module in cmd/server.
type GarageReader interface {
	// Get returns the garage's current letterhead, for a document that has
	// no snapshot of its own.
	Get(ctx context.Context, garageID string) (GarageHeaderInfo, error)
	// Logo returns the content of a logo by its object key.
	Logo(ctx context.Context, key string) ([]byte, error)
}

// DocumentUseCase implements every export of the Document/Print module.
type DocumentUseCase struct {
	Quotes         QuoteReader
	RepairOrders   RepairOrderReader
	Settlements    SettlementReader
	StockMovements StockMovementReader
	Revenue        RevenueReader
	Garages        GarageReader
	Inventory      InventoryDocumentReader
}

// NewDocumentUseCase wires the use case to its dependencies.
func NewDocumentUseCase(
	quotes QuoteReader, repairOrders RepairOrderReader, settlements SettlementReader, stockMovements StockMovementReader,
	revenue RevenueReader, garages GarageReader, inventory InventoryDocumentReader,
) *DocumentUseCase {
	return &DocumentUseCase{
		Quotes: quotes, RepairOrders: repairOrders, Settlements: settlements, StockMovements: stockMovements,
		Revenue: revenue, Garages: garages, Inventory: inventory,
	}
}

// QuotePDF renders a quote as a PDF (FR-DOC-01).
//
//nolint:dupl // each export reads one kind of document and renders it with its own layout
func (uc *DocumentUseCase) QuotePDF(ctx context.Context, garageID, quoteID string) ([]byte, error) {
	quote, err := uc.Quotes.Get(ctx, garageID, quoteID)
	if err != nil {
		return nil, toAppError("export quote pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, quote.Issuer)
	if err != nil {
		return nil, err
	}

	pdf, err := render.QuotePDF(render.QuotePDFData{
		Garage: garage, BillTo: billTo(quote.BillingParty), Number: quote.Number, Status: quote.Status, CustomerName: quote.CustomerName,
		VehiclePlate: quote.VehiclePlate, Notes: quote.Notes, Lines: quote.Lines, TotalAmount: quote.TotalAmount,
	})
	if err != nil {
		return nil, apperror.Internal("export quote pdf", err)
	}

	return pdf, nil
}

// RepairOrderPDF renders a repair order as a PDF (FR-DOC-01).
func (uc *DocumentUseCase) RepairOrderPDF(ctx context.Context, garageID, repairOrderID string) ([]byte, error) {
	ro, err := uc.RepairOrders.Get(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError("export repair order pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, ro.Issuer)
	if err != nil {
		return nil, err
	}

	pdf, err := render.RepairOrderPDF(render.RepairOrderPDFData{
		Garage: garage, BillTo: billTo(ro.BillingParty), Number: ro.Number, Status: ro.Status, CustomerName: ro.CustomerName,
		VehiclePlate: ro.VehiclePlate, Lines: ro.Lines, TotalAmount: ro.TotalAmount,
	})
	if err != nil {
		return nil, apperror.Internal("export repair order pdf", err)
	}

	return pdf, nil
}

// SettlementReceiptPDF renders a settlement as a receipt PDF (FR-DOC-01).
//
//nolint:dupl // each export reads one kind of document and renders it with its own layout
func (uc *DocumentUseCase) SettlementReceiptPDF(ctx context.Context, garageID, settlementID string) ([]byte, error) {
	settlement, err := uc.Settlements.Get(ctx, garageID, settlementID)
	if err != nil {
		return nil, toAppError("export settlement receipt pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, settlement.Issuer)
	if err != nil {
		return nil, err
	}

	pdf, err := render.SettlementReceiptPDF(render.SettlementReceiptData{
		Garage: garage, BillTo: billTo(settlement.BillingParty), Number: settlement.Number, Status: settlement.Status,
		CustomerName: settlement.CustomerName,
		VehiclePlate: settlement.VehiclePlate, TotalAmount: settlement.TotalAmount,
		PaidAmount: settlement.PaidAmount, Payments: settlement.Payments,
	})
	if err != nil {
		return nil, apperror.Internal("export settlement receipt pdf", err)
	}

	return pdf, nil
}

// PaymentVoucherPDF renders one payment voucher as a PDF in the 01-TT
// layout (FR-BIL-05), from the legal identity it was issued with.
func (uc *DocumentUseCase) PaymentVoucherPDF(ctx context.Context, garageID, settlementID, paymentID string) ([]byte, error) {
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

	pdf, err := render.PaymentVoucherPDF(render.VoucherPDFData{
		Garage: garage, Number: voucher.Number, Date: voucher.Date, PayerName: voucher.PayerName,
		PayerAddress: voucher.PayerAddress, Reason: voucher.Reason, Amount: voucher.Amount, Method: voucher.Method,
		SettlementNumber: settlement.Number, CollectorName: voucher.CollectorName,
		Voided: voucher.Voided, VoidReason: voucher.VoidReason,
	})
	if err != nil {
		return nil, apperror.Internal("export payment voucher pdf", err)
	}

	return pdf, nil
}

func findVoucher(vouchers []VoucherInfo, paymentID string) (VoucherInfo, bool) {
	for _, v := range vouchers {
		if v.ID == paymentID {
			return v, true
		}
	}

	return VoucherInfo{}, false
}

// StockMovementsExcel renders the stock movement report as an Excel
// workbook (FR-DOC-02), the same data and filter as
// InventoryService.ListStockTransactions.
func (uc *DocumentUseCase) StockMovementsExcel(ctx context.Context, garageID, itemID string) ([]byte, error) {
	rows, err := uc.StockMovements.List(ctx, garageID, itemID)
	if err != nil {
		return nil, toAppError("export stock movements excel", err)
	}

	workbook, err := render.StockMovementsExcel(rows)
	if err != nil {
		return nil, apperror.Internal("export stock movements excel", err)
	}

	return workbook, nil
}

// RevenueExcel renders the revenue-by-period report as an Excel workbook
// (FR-DOC-02), the same data and range as ReportingService.GetRevenueReport.
func (uc *DocumentUseCase) RevenueExcel(ctx context.Context, garageID, period string, from, to time.Time) ([]byte, error) {
	info, err := uc.Revenue.Revenue(ctx, garageID, period, from, to)
	if err != nil {
		return nil, toAppError("export revenue excel", err)
	}

	workbook, err := render.RevenueExcel(info.Points, info.TotalAmount)
	if err != nil {
		return nil, apperror.Internal("export revenue excel", err)
	}

	return workbook, nil
}

// GoodsReceiptPDF renders a goods receipt as a PDF (FR-INV-06).
func (uc *DocumentUseCase) GoodsReceiptPDF(ctx context.Context, garageID, receiptID string) ([]byte, error) {
	receipt, err := uc.Inventory.GoodsReceipt(ctx, garageID, receiptID)
	if err != nil {
		return nil, toAppError("export goods receipt pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, issuer.Issuer{})
	if err != nil {
		return nil, err
	}

	pdf, err := render.GoodsReceiptPDF(render.GoodsReceiptPDFData{
		Garage: garage, Number: receipt.Number, Status: receipt.Status, Date: receipt.Date,
		DeliveryNoteNumber: receipt.DeliveryNoteNumber, Notes: receipt.Notes, Lines: receipt.Lines,
		ShowCost: receipt.ShowCost, TotalAmount: receipt.TotalAmount,
	})
	if err != nil {
		return nil, apperror.Internal("export goods receipt pdf", err)
	}

	return pdf, nil
}

// StockIssuePDF renders a repair order's stock issue (phiếu xuất kho) as a
// PDF (FR-INV-20).
func (uc *DocumentUseCase) StockIssuePDF(ctx context.Context, garageID, repairOrderID string) ([]byte, error) {
	issue, err := uc.Inventory.StockIssue(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError("export stock issue pdf", err)
	}

	garage, err := uc.letterhead(ctx, garageID, issuer.Issuer{})
	if err != nil {
		return nil, err
	}

	pdf, err := render.StockIssuePDF(render.StockIssuePDFData{
		Garage: garage, Number: issue.Number, Status: issue.Status, RepairOrderNumber: issue.RepairOrderNumber,
		PostedAt: issue.PostedAt, PostedBy: issue.PostedBy, Lines: issue.Lines,
	})
	if err != nil {
		return nil, apperror.Internal("export stock issue pdf", err)
	}

	return pdf, nil
}

// PurchasesExcel renders the purchases-by-supplier report as an Excel
// workbook (FR-INV-19), the same data and filters as
// InventoryService.GetPurchasesReport.
func (uc *DocumentUseCase) PurchasesExcel(ctx context.Context, garageID, supplierID, from, to string) ([]byte, error) {
	rows, total, err := uc.Inventory.Purchases(ctx, garageID, supplierID, from, to)
	if err != nil {
		return nil, toAppError("export purchases excel", err)
	}

	workbook, err := render.PurchasesExcel(rows, total)
	if err != nil {
		return nil, apperror.Internal("export purchases excel", err)
	}

	return workbook, nil
}

// StockValuationExcel renders the stock valuation report as an Excel
// workbook (FR-INV-07), the same data as
// InventoryService.GetStockValuationReport.
func (uc *DocumentUseCase) StockValuationExcel(ctx context.Context, garageID, at string) ([]byte, error) {
	rows, total, err := uc.Inventory.Valuation(ctx, garageID, at)
	if err != nil {
		return nil, toAppError("export stock valuation excel", err)
	}

	workbook, err := render.StockValuationExcel(rows, total)
	if err != nil {
		return nil, apperror.Internal("export stock valuation excel", err)
	}

	return workbook, nil
}

// letterhead returns the header a document prints: the garage's legal
// identity as it was when the document was issued (FR-TEN-06) or, for a
// document without a snapshot, the garage's current profile. A logo that
// cannot be read is left out.
func (uc *DocumentUseCase) letterhead(ctx context.Context, garageID string, snapshot issuer.Issuer) (render.GarageHeader, error) {
	if snapshot.IsZero() {
		g, err := uc.Garages.Get(ctx, garageID)
		if err != nil {
			return render.GarageHeader{}, apperror.Internal("read garage", err)
		}

		return render.GarageHeader{
			Name: g.Name, Address: g.Address, Phone: g.Phone, TaxCode: g.TaxCode, Representative: g.Representative,
			Bank: g.Bank, Logo: g.Logo,
		}, nil
	}

	header := render.GarageHeader{
		Name: snapshot.Name, Address: snapshot.Address, Phone: snapshot.Phone, TaxCode: snapshot.TaxCode,
		Representative: snapshot.Representative, Bank: snapshot.BankLine(),
	}

	if snapshot.LogoKey != "" {
		logo, err := uc.Garages.Logo(ctx, snapshot.LogoKey)
		if err != nil {
			slog.Warn("document: reading the logo failed", "garage_id", garageID, "key", snapshot.LogoKey, "error", err)
		} else {
			header.Logo = logo
		}
	}

	return header, nil
}

// billTo is the party a document prints under its header.
func billTo(p billingparty.Party) render.BillTo {
	return render.BillTo{Name: p.LegalName, TaxID: p.TaxID, Address: p.Address}
}

// toAppError passes an already-classified error (from a reader adapter)
// through unchanged, or wraps an unexpected one as internal -- this
// module has no domain errors of its own to classify against, unlike a
// module with its own business rules.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	return apperror.Internal(op, err)
}
