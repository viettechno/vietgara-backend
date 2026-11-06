package usecase

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/pkg/numbering"
)

// The numbering of payment-out vouchers is fixed: unlike the settlement and
// payment voucher templates, the garage does not edit it.
const (
	docTypePaymentOut = "payment_out"
	// PaymentOutNumberTemplate numbers payment-out vouchers.
	PaymentOutNumberTemplate = "PC-{YYYY}-{SEQ:5}"
)

// Audited actions of payment-out vouchers.
const (
	ActionVoucherIssued = "payment_out_voucher.issued"
	ActionVoucherVoided = "payment_out_voucher.voided"

	entityVoucher = "payment_out_voucher"
)

var validMethods = map[string]bool{"cash": true, "bank_transfer": true, "online": true}

// AllocationInput applies part of a voucher to one payable.
type AllocationInput struct {
	PayableID string
	Amount    int64
}

// CreateVoucherRequest is the input of CreateVoucher. Payee, address and
// reason default to the supplier's name and address and a standard text.
type CreateVoucherRequest struct {
	GarageID     string
	ActorID      string
	SupplierID   string
	PayeeName    string
	PayeeAddress string
	Amount       int64
	Method       string
	Reason       string
	Note         string
	Allocations  []AllocationInput
}

// CreateVoucher issues a payment-out voucher and applies its allocations to
// the supplier's payables, in the caller's transaction (FR-BIL-07).
func (uc *PayableUseCase) CreateVoucher(ctx context.Context, req CreateVoucherRequest) (*domain.PaymentOutVoucher, error) {
	const op = "create payment-out voucher"

	if err := validateVoucher(&req); err != nil {
		return nil, toAppError(op, err)
	}

	supplier, err := uc.suppliers.Get(ctx, req.GarageID, req.SupplierID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	payables, err := uc.lockAllocated(ctx, req)
	if err != nil {
		return nil, err
	}

	v, err := uc.buildVoucher(ctx, req, supplier, payables)
	if err != nil {
		return nil, err
	}

	if err := uc.repo.AddVoucher(ctx, v); err != nil {
		return nil, toAppError(op, err)
	}

	for _, p := range payables {
		p.PaidAmount += allocatedTo(req.Allocations, p.ID)
		p.Derive()

		if err := uc.repo.SavePayable(ctx, p); err != nil {
			return nil, toAppError(op, err)
		}
	}

	if err := recordAudit(ctx, uc.audit, req.ActorID, req.GarageID, ActionVoucherIssued, entityVoucher, v.ID, nil, voucherAudit(v)); err != nil {
		return nil, err
	}

	return v, nil
}

// validateVoucher checks what can be checked without reading anything.
func validateVoucher(req *CreateVoucherRequest) error {
	supplierID, err := uuid.Parse(req.SupplierID)
	if err != nil {
		return domain.ErrSupplierNotFound
	}

	// Ids are compared as text further on, so keep only the canonical form.
	req.SupplierID = supplierID.String()

	if !validMethods[req.Method] {
		return domain.ErrInvalidPaymentOutMethod
	}

	req.PayeeName, req.PayeeAddress = strings.TrimSpace(req.PayeeName), strings.TrimSpace(req.PayeeAddress)
	req.Reason, req.Note = strings.TrimSpace(req.Reason), strings.TrimSpace(req.Note)

	if utf8.RuneCountInString(req.PayeeName) > domain.MaxPayeeNameLength ||
		utf8.RuneCountInString(req.PayeeAddress) > domain.MaxPayeeAddressLength ||
		utf8.RuneCountInString(req.Reason) > domain.MaxReasonLength ||
		utf8.RuneCountInString(req.Note) > domain.MaxNoteLength {
		return domain.ErrInvalidPaymentOutVoucher
	}

	sum, err := canonicalAllocations(req.Allocations)
	if err != nil {
		return err
	}

	if req.Amount <= 0 || sum > req.Amount {
		return domain.ErrInvalidPaymentOutAmount
	}

	return nil
}

// canonicalAllocations checks the allocations of a voucher, puts their payable
// ids in canonical form and returns their sum.
func canonicalAllocations(allocations []AllocationInput) (int64, error) {
	var sum int64

	seen := map[string]bool{}

	for i, a := range allocations {
		payableID, err := uuid.Parse(a.PayableID)
		if err != nil || a.Amount <= 0 || a.Amount > math.MaxInt64-sum || seen[payableID.String()] {
			return 0, domain.ErrInvalidAllocation
		}

		allocations[i].PayableID = payableID.String()
		seen[payableID.String()] = true
		sum += a.Amount
	}

	return sum, nil
}

// lockAllocated locks the payables the allocations go to and checks each
// belongs to the voucher's supplier, is open and has room for its amount.
// The lock makes two vouchers that go to one payable take turns.
func (uc *PayableUseCase) lockAllocated(ctx context.Context, req CreateVoucherRequest) ([]*domain.Payable, error) {
	if len(req.Allocations) == 0 {
		return nil, nil
	}

	ids := make([]string, len(req.Allocations))
	for i, a := range req.Allocations {
		ids[i] = a.PayableID
	}

	payables, err := uc.repo.LockPayables(ctx, req.GarageID, ids)
	if err != nil {
		return nil, toAppError("lock payables", err)
	}

	if len(payables) != len(ids) {
		return nil, toAppError("create payment-out voucher", domain.ErrInvalidAllocation)
	}

	for _, p := range payables {
		if p.SupplierID != req.SupplierID || p.Status != domain.PayableOpen || allocatedTo(req.Allocations, p.ID) > p.Remaining() {
			return nil, toAppError("create payment-out voucher", domain.ErrInvalidAllocation)
		}
	}

	return payables, nil
}

func allocatedTo(allocations []AllocationInput, payableID string) int64 {
	for _, a := range allocations {
		if a.PayableID == payableID {
			return a.Amount
		}
	}

	return 0
}

// buildVoucher numbers the voucher and snapshots who issued it and the
// garage's legal identity.
func (uc *PayableUseCase) buildVoucher(ctx context.Context, req CreateVoucherRequest, supplier SupplierView, payables []*domain.Payable) (*domain.PaymentOutVoucher, error) {
	const op = "create payment-out voucher"

	loc, err := locationOf(ctx, uc.garages, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	number, err := uc.nextNumber(ctx, req.GarageID, loc)
	if err != nil {
		return nil, toAppError(op, err)
	}

	legal, err := uc.issuers.Issuer(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	snapshot, err := json.Marshal(legal)
	if err != nil {
		return nil, toAppError(op, err)
	}

	name, err := uc.staff.FullName(ctx, req.ActorID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	v := &domain.PaymentOutVoucher{
		ID: uuid.NewString(), GarageID: req.GarageID, Number: number, SupplierID: req.SupplierID, SupplierName: supplier.Name,
		PayeeName: firstNonBlank(req.PayeeName, supplier.Name), PayeeAddress: firstNonBlank(req.PayeeAddress, supplier.Address),
		Amount: req.Amount, Method: req.Method, Reason: req.Reason, Note: req.Note, IssuerName: name, Issuer: snapshot,
		CreatedBy: req.ActorID,
	}

	for _, p := range payables {
		v.Allocations = append(v.Allocations, domain.Allocation{
			PayableID: p.ID, GoodsReceiptNumber: p.GoodsReceiptNumber, Amount: allocatedTo(req.Allocations, p.ID),
		})
	}

	if v.Reason == "" {
		v.Reason = defaultReason(v.Allocations)
	}

	return v, nil
}

// defaultReason names the receipts a voucher pays, as far as it fits.
func defaultReason(allocations []domain.Allocation) string {
	reason := "Thanh toán tiền hàng"
	if len(allocations) == 0 {
		return reason
	}

	numbers := make([]string, len(allocations))
	for i, a := range allocations {
		numbers[i] = a.GoodsReceiptNumber
	}

	reason += " theo phiếu nhập " + strings.Join(numbers, ", ")
	if utf8.RuneCountInString(reason) > domain.MaxReasonLength {
		return string([]rune(reason)[:domain.MaxReasonLength])
	}

	return reason
}

func (uc *PayableUseCase) nextNumber(ctx context.Context, garageID string, loc *time.Location) (string, error) {
	now := uc.now().In(loc)

	seq, err := uc.sequences.Next(ctx, garageID, docTypePaymentOut, numbering.Period(PaymentOutNumberTemplate, now))
	if err != nil {
		return "", err
	}

	return numbering.Render(PaymentOutNumberTemplate, now, seq), nil
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}

	return ""
}

// VoidVoucherRequest is the input of VoidVoucher.
type VoidVoucherRequest struct {
	GarageID  string
	VoucherID string
	ActorID   string
	Reason    string
}

// VoidVoucher voids a voucher: it stays on file with its number and reason,
// and its allocations come off the payables that still stand (FR-BIL-07).
func (uc *PayableUseCase) VoidVoucher(ctx context.Context, req VoidVoucherRequest) (*domain.PaymentOutVoucher, error) {
	const op = "void payment-out voucher"

	reason := strings.TrimSpace(req.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > domain.MaxVoidReasonLength {
		return nil, toAppError(op, domain.ErrInvalidPaymentOutVoidNote)
	}

	if _, err := uuid.Parse(req.VoucherID); err != nil {
		return nil, toAppError(op, domain.ErrVoucherNotFound)
	}

	// The update that marks it voided comes first: two voids of one voucher
	// take turns on its row, and the second finds it voided.
	if err := uc.repo.VoidVoucher(ctx, req.GarageID, req.VoucherID, reason, uc.now()); err != nil {
		return nil, toAppError(op, err)
	}

	v, err := uc.repo.FindVoucher(ctx, req.GarageID, req.VoucherID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.releaseAllocations(ctx, v); err != nil {
		return nil, err
	}

	err = recordAudit(ctx, uc.audit, req.ActorID, req.GarageID, ActionVoucherVoided, entityVoucher, v.ID,
		map[string]any{"number": v.Number, "amount": v.Amount}, map[string]any{"number": v.Number, "amount": v.Amount, "voided": true, "void_reason": reason})
	if err != nil {
		return nil, err
	}

	return v, nil
}

// releaseAllocations takes a voided voucher's allocations off the payables.
// A payable cancelled in the meantime is left as it is.
func (uc *PayableUseCase) releaseAllocations(ctx context.Context, v *domain.PaymentOutVoucher) error {
	if len(v.Allocations) == 0 {
		return nil
	}

	ids := make([]string, len(v.Allocations))
	for i, a := range v.Allocations {
		ids[i] = a.PayableID
	}

	payables, err := uc.repo.LockPayables(ctx, v.GarageID, ids)
	if err != nil {
		return toAppError("void payment-out voucher", err)
	}

	for _, p := range payables {
		if p.Status == domain.PayableCancelled {
			continue
		}

		for _, a := range v.Allocations {
			if a.PayableID != p.ID {
				continue
			}

			if p.PaidAmount < a.Amount {
				slog.Error("receivable: payable paid less than its voided allocation", "payable_id", p.ID, "paid", p.PaidAmount, "allocated", a.Amount)
			}

			p.PaidAmount = max(0, p.PaidAmount-a.Amount)
		}

		p.Derive()

		if err := uc.repo.SavePayable(ctx, p); err != nil {
			return toAppError("void payment-out voucher", err)
		}
	}

	return nil
}

func voucherAudit(v *domain.PaymentOutVoucher) map[string]any {
	return map[string]any{
		"number": v.Number, "supplier_id": v.SupplierID, "amount": v.Amount, "method": v.Method,
		"allocated": v.Allocated(), "allocations": len(v.Allocations),
	}
}

// ListVouchersRequest is the input of ListVouchers.
type ListVouchersRequest struct {
	GarageID      string
	SupplierID    string
	IncludeVoided bool
	PageRequest
}

// ListVouchersResult is one page of vouchers.
type ListVouchersResult struct {
	Items    []*domain.PaymentOutVoucher
	Total    int
	Page     int
	PageSize int
}

// ListVouchers lists the vouchers, newest first.
func (uc *PayableUseCase) ListVouchers(ctx context.Context, req ListVouchersRequest) (*ListVouchersResult, error) {
	page := req.normalize()

	if err := validateOptionalID("supplierId", req.SupplierID); err != nil {
		return nil, err
	}

	items, total, err := uc.repo.ListVouchers(ctx, req.GarageID, VoucherFilter{
		SupplierID: req.SupplierID, IncludeVoided: req.IncludeVoided, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list payment-out vouchers", err)
	}

	return &ListVouchersResult{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// GetVoucher returns one voucher with its allocations.
func (uc *PayableUseCase) GetVoucher(ctx context.Context, garageID, voucherID string) (*domain.PaymentOutVoucher, error) {
	if _, err := uuid.Parse(voucherID); err != nil {
		return nil, toAppError("get payment-out voucher", domain.ErrVoucherNotFound)
	}

	v, err := uc.repo.FindVoucher(ctx, garageID, voucherID)
	if err != nil {
		return nil, toAppError("get payment-out voucher", err)
	}

	return v, nil
}
