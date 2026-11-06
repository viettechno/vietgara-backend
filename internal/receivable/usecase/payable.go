package usecase

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// PayableDeps are the dependencies of PayableUseCase.
type PayableDeps struct {
	Repo      PayableRepository
	Suppliers SupplierReader
	Garages   GarageReader
	Issuers   IssuerReader
	Staff     StaffReader
	Sequences Sequencer
	Audit     AuditLogger
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// PayableUseCase is the Receivables module's application service for supplier
// payables and the payment-out vouchers that settle them (FR-INV-17,
// FR-BIL-07).
type PayableUseCase struct {
	repo      PayableRepository
	suppliers SupplierReader
	garages   GarageReader
	issuers   IssuerReader
	staff     StaffReader
	sequences Sequencer
	audit     AuditLogger
	now       clock
}

// NewPayableUseCase wires the use case.
func NewPayableUseCase(d PayableDeps) *PayableUseCase {
	now := d.Now
	if now == nil {
		now = time.Now
	}

	return &PayableUseCase{
		repo: d.Repo, suppliers: d.Suppliers, garages: d.Garages, issuers: d.Issuers, staff: d.Staff,
		sequences: d.Sequences, audit: d.Audit, now: now,
	}
}

// receiptCompleted is goods_receipt.completed: what each supplier of the
// receipt is owed.
type receiptCompleted struct {
	GoodsReceiptID string `json:"goods_receipt_id"`
	Number         string `json:"number"`
	ReceiptDate    string `json:"receipt_date"`
	Suppliers      []struct {
		SupplierID   string `json:"supplier_id"`
		SupplierName string `json:"supplier_name"`
		Amount       int64  `json:"amount"`
	} `json:"suppliers"`
}

// HandleReceiptCompleted makes a payable per supplier of a completed goods
// receipt. A payable already made for the receipt and supplier is left as it
// is, so a redelivered event adds nothing.
func (uc *PayableUseCase) HandleReceiptCompleted(ctx context.Context, event outbox.Event) error {
	var p receiptCompleted
	if err := json.Unmarshal(event.Data, &p); err != nil || event.GarageID == "" || p.GoodsReceiptID == "" {
		slog.Error("receivable: dropped a malformed goods_receipt.completed event", "event_id", event.ID, "error", err)

		return nil
	}

	date, err := time.Parse(time.DateOnly, p.ReceiptDate)
	if err != nil {
		slog.Error("receivable: dropped a goods_receipt.completed event with a bad date", "event_id", event.ID, "error", err)

		return nil
	}

	for _, s := range p.Suppliers {
		if s.Amount <= 0 || s.SupplierID == "" {
			continue
		}

		err := uc.repo.AddPayable(ctx, &domain.Payable{
			GarageID: event.GarageID, GoodsReceiptID: p.GoodsReceiptID, GoodsReceiptNumber: p.Number,
			SupplierID: s.SupplierID, SupplierName: s.SupplierName, ReceiptDate: date, TotalAmount: s.Amount,
			Status: domain.PayableOpen,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// HandleReceiptCancelled cancels the payables of a goods receipt that was
// reversed.
func (uc *PayableUseCase) HandleReceiptCancelled(ctx context.Context, event outbox.Event) error {
	var p struct {
		GoodsReceiptID string `json:"goods_receipt_id"`
	}

	if err := json.Unmarshal(event.Data, &p); err != nil || event.GarageID == "" || p.GoodsReceiptID == "" {
		slog.Error("receivable: dropped a malformed goods_receipt.cancelled event", "event_id", event.ID, "error", err)

		return nil
	}

	return uc.repo.CancelPayables(ctx, event.GarageID, p.GoodsReceiptID)
}

// PayableItem is a payable with its age.
type PayableItem struct {
	Payable *domain.Payable
	AgeDays int
}

// ListPayablesRequest is the input of ListPayables.
type ListPayablesRequest struct {
	GarageID    string
	SupplierID  string
	IncludePaid bool
	PageRequest
}

// ListPayablesResult is one page of payables with the summary of the match.
type ListPayablesResult struct {
	Items    []PayableItem
	Total    int
	Page     int
	PageSize int
	Summary  PayableSummary
}

func (uc *PayableUseCase) today(ctx context.Context, garageID string) (time.Time, error) {
	loc, err := locationOf(ctx, uc.garages, garageID)
	if err != nil {
		return time.Time{}, err
	}

	return domain.Today(uc.now(), loc), nil
}

// ListPayables lists the payables, oldest first (FR-INV-17).
func (uc *PayableUseCase) ListPayables(ctx context.Context, req ListPayablesRequest) (*ListPayablesResult, error) {
	const op = "list supplier payables"

	page := req.normalize()

	if err := validateOptionalID("supplierId", req.SupplierID); err != nil {
		return nil, err
	}

	today, err := uc.today(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	payables, total, summary, err := uc.repo.ListPayables(ctx, req.GarageID, PayableFilter{
		SupplierID: req.SupplierID, IncludePaid: req.IncludePaid, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError(op, err)
	}

	items := make([]PayableItem, len(payables))
	for i, p := range payables {
		items[i] = PayableItem{Payable: p, AgeDays: p.AgeDays(today)}
	}

	return &ListPayablesResult{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize, Summary: summary}, nil
}

// AgingResult is the open payables by supplier and age, with the totals.
type AgingResult struct {
	Suppliers []domain.Aging
	Totals    domain.Aging
}

// Aging returns the open payables by supplier and age of the receipt.
func (uc *PayableUseCase) Aging(ctx context.Context, garageID string) (*AgingResult, error) {
	const op = "supplier payables aging"

	today, err := uc.today(ctx, garageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	suppliers, err := uc.repo.Aging(ctx, garageID, today)
	if err != nil {
		return nil, toAppError(op, err)
	}

	res := &AgingResult{Suppliers: suppliers}

	for _, s := range suppliers {
		res.Totals.Total += s.Total
		res.Totals.Current += s.Current
		res.Totals.Days31to60 += s.Days31to60
		res.Totals.Days61to90 += s.Days61to90
		res.Totals.Over90 += s.Over90
	}

	return res, nil
}

// validateOptionalID refuses a non-empty value that is not an id.
func validateOptionalID(field, value string) error {
	if value == "" {
		return nil
	}

	if _, err := uuid.Parse(value); err != nil {
		return apperror.Validation(field, apperror.IssueInvalid, field+" must be an id")
	}

	return nil
}
