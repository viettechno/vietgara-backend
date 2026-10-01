// Package usecase renders the documents of the Document/Print module
// (FRD 3.13): a quote PDF, a settlement receipt PDF (FR-DOC-01), and the
// stock movement report as an Excel workbook (FR-DOC-02). It owns no
// tables of its own -- every document is generated fresh from the owning
// module's own data, read through narrow reader interfaces implemented
// by adapters in cmd/server, the same cross-module decoupling pattern
// used everywhere else (HLD Section 2).
package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/document/render"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// QuoteInfo is the Quote & Pricing module's view of a quote that
// Document/Print needs to render its PDF.
type QuoteInfo struct {
	Number, Status, CustomerName, VehiclePlate, Notes string
	Lines                                             []render.DocumentLine
	TotalAmount                                       int64
}

// QuoteReader reads a quote for PDF export; implemented by an adapter
// over the Quote & Pricing module in cmd/server. Returns an
// *apperror.AppError directly (e.g. apperror.NotFound) rather than a
// module-specific sentinel -- this module has no domain errors of its
// own to translate into, unlike a module with its own business rules.
type QuoteReader interface {
	Get(ctx context.Context, garageID, quoteID string) (QuoteInfo, error)
}

// SettlementInfo is the Billing & Settlement module's view of a
// settlement that Document/Print needs to render its receipt PDF.
type SettlementInfo struct {
	Number, Status, CustomerName, VehiclePlate, InsurerName string
	TotalAmount, CustomerAmount, InsurerAmount              int64
	Payments                                                []render.PaymentRow
}

// SettlementReader reads a settlement for receipt PDF export; implemented
// by an adapter over the Billing & Settlement module in cmd/server.
type SettlementReader interface {
	Get(ctx context.Context, garageID, settlementID string) (SettlementInfo, error)
}

// StockMovementReader reads the stock movement report for Excel export;
// implemented by an adapter over the Inventory module in cmd/server.
type StockMovementReader interface {
	List(ctx context.Context, garageID, itemID string) ([]render.StockMovementRow, error)
}

// GarageHeaderInfo is the Tenant module's view of a garage that every
// document's letterhead needs.
type GarageHeaderInfo struct {
	Name, Address, Phone string
}

// GarageReader reads a garage's letterhead; implemented by an adapter
// over the Tenant module in cmd/server.
type GarageReader interface {
	Get(ctx context.Context, garageID string) (GarageHeaderInfo, error)
}

// DocumentUseCase implements every export of the Document/Print module.
type DocumentUseCase struct {
	Quotes         QuoteReader
	Settlements    SettlementReader
	StockMovements StockMovementReader
	Garages        GarageReader
}

// NewDocumentUseCase wires the use case to its dependencies.
func NewDocumentUseCase(quotes QuoteReader, settlements SettlementReader, stockMovements StockMovementReader, garages GarageReader) *DocumentUseCase {
	return &DocumentUseCase{Quotes: quotes, Settlements: settlements, StockMovements: stockMovements, Garages: garages}
}

// QuotePDF renders a quote as a PDF (FR-DOC-01).
func (uc *DocumentUseCase) QuotePDF(ctx context.Context, garageID, quoteID string) ([]byte, error) {
	garage, err := uc.garageHeader(ctx, garageID)
	if err != nil {
		return nil, err
	}

	quote, err := uc.Quotes.Get(ctx, garageID, quoteID)
	if err != nil {
		return nil, toAppError("export quote pdf", err)
	}

	pdf, err := render.QuotePDF(render.QuotePDFData{
		Garage: garage, Number: quote.Number, Status: quote.Status, CustomerName: quote.CustomerName,
		VehiclePlate: quote.VehiclePlate, Notes: quote.Notes, Lines: quote.Lines, TotalAmount: quote.TotalAmount,
	})
	if err != nil {
		return nil, apperror.Internal("export quote pdf", err)
	}

	return pdf, nil
}

// SettlementReceiptPDF renders a settlement as a receipt PDF (FR-DOC-01).
func (uc *DocumentUseCase) SettlementReceiptPDF(ctx context.Context, garageID, settlementID string) ([]byte, error) {
	garage, err := uc.garageHeader(ctx, garageID)
	if err != nil {
		return nil, err
	}

	settlement, err := uc.Settlements.Get(ctx, garageID, settlementID)
	if err != nil {
		return nil, toAppError("export settlement receipt pdf", err)
	}

	pdf, err := render.SettlementReceiptPDF(render.SettlementReceiptData{
		Garage: garage, Number: settlement.Number, Status: settlement.Status, CustomerName: settlement.CustomerName,
		VehiclePlate: settlement.VehiclePlate, TotalAmount: settlement.TotalAmount,
		CustomerAmount: settlement.CustomerAmount, InsurerAmount: settlement.InsurerAmount,
		InsurerName: settlement.InsurerName, Payments: settlement.Payments,
	})
	if err != nil {
		return nil, apperror.Internal("export settlement receipt pdf", err)
	}

	return pdf, nil
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

func (uc *DocumentUseCase) garageHeader(ctx context.Context, garageID string) (render.GarageHeader, error) {
	g, err := uc.Garages.Get(ctx, garageID)
	if err != nil {
		return render.GarageHeader{}, apperror.Internal("read garage", err)
	}

	return render.GarageHeader{Name: g.Name, Address: g.Address, Phone: g.Phone}, nil
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
