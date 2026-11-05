package domain

import (
	"errors"
	"time"
)

// PayableStatus is the payment state of one supplier payable.
type PayableStatus string

// The payable statuses.
const (
	PayableOpen PayableStatus = "open"
	PayablePaid PayableStatus = "paid"
	// PayableCancelled: its goods receipt was cancelled; it leaves the lists
	// and the aging.
	PayableCancelled PayableStatus = "cancelled"
)

// Errors of payables and payment-out vouchers.
var (
	ErrPayableNotFound           = errors.New("supplier payable not found")
	ErrVoucherNotFound           = errors.New("payment-out voucher not found")
	ErrSupplierNotFound          = errors.New("supplier not found")
	ErrInvalidPaymentOutAmount   = errors.New("the amount must be positive and at least the sum of the allocations")
	ErrInvalidAllocation         = errors.New("an allocation must go to an open payable of the supplier, once, for at most its remaining amount")
	ErrInvalidPaymentOutMethod   = errors.New("the payment method is not valid")
	ErrInvalidPaymentOutVoucher  = errors.New("the payee, address, reason or note is too long")
	ErrInvalidPaymentOutVoidNote = errors.New("a void reason of 1 to 500 characters is required")
	ErrPaymentOutAlreadyVoided   = errors.New("the payment-out voucher is already voided")
)

// Limits of a payment-out voucher's text fields, as on a payment voucher.
const (
	MaxPayeeNameLength    = 255
	MaxPayeeAddressLength = 500
	MaxReasonLength       = 500
	MaxNoteLength         = 1000
	MaxVoidReasonLength   = 500
)

// Payable is what the garage owes one supplier for the lines of one
// completed goods receipt.
type Payable struct {
	ID                 string
	GarageID           string
	GoodsReceiptID     string
	GoodsReceiptNumber string
	SupplierID         string
	SupplierName       string
	// ReceiptDate is the receipt's day; the age counts from it.
	ReceiptDate time.Time
	TotalAmount int64
	PaidAmount  int64
	Status      PayableStatus
	CreatedAt   time.Time
}

// Remaining is what is still owed.
func (p *Payable) Remaining() int64 { return p.TotalAmount - p.PaidAmount }

// Derive sets open or paid from the amounts; a cancelled payable stays
// cancelled.
func (p *Payable) Derive() {
	switch {
	case p.Status == PayableCancelled:
	case p.PaidAmount >= p.TotalAmount:
		p.Status = PayablePaid
	default:
		p.Status = PayableOpen
	}
}

// AgeDays is the whole days from the receipt's day to today, never negative.
func (p *Payable) AgeDays(today time.Time) int {
	return max(0, int(today.Sub(p.ReceiptDate).Hours()/24))
}

// Aging is the open payables of one supplier by age of the receipt.
type Aging struct {
	SupplierID   string
	SupplierName string
	Total        int64
	Current      int64
	Days31to60   int64
	Days61to90   int64
	Over90       int64
}

// Add counts one open payable's remaining amount in its age bucket.
func (a *Aging) Add(remaining int64, ageDays int) {
	a.Total += remaining

	switch {
	case ageDays <= 30:
		a.Current += remaining
	case ageDays <= 60:
		a.Days31to60 += remaining
	case ageDays <= 90:
		a.Days61to90 += remaining
	default:
		a.Over90 += remaining
	}
}

// Allocation applies part of a voucher to one payable.
type Allocation struct {
	PayableID          string
	GoodsReceiptNumber string
	Amount             int64
	// Cancelled marks an allocation to a payable whose goods receipt was
	// cancelled: it counts as unallocated.
	Cancelled bool
}

// PaymentOutVoucher is a payment-out voucher (phiếu chi): money the garage
// paid out to a supplier.
type PaymentOutVoucher struct {
	ID           string
	GarageID     string
	Number       string
	SupplierID   string
	SupplierName string
	PayeeName    string
	PayeeAddress string
	Amount       int64
	Method       string
	Reason       string
	Note         string
	Allocations  []Allocation
	IssuerName   string
	// Issuer is the garage's legal identity as issued, a JSON snapshot.
	Issuer     []byte
	VoidReason string
	VoidedAt   *time.Time
	CreatedAt  time.Time
	CreatedBy  string
}

// Voided reports whether the voucher was voided.
func (v *PaymentOutVoucher) Voided() bool { return v.VoidedAt != nil }

// Allocated is the sum of the allocations to payables that still stand.
func (v *PaymentOutVoucher) Allocated() int64 {
	var sum int64

	for _, a := range v.Allocations {
		if !a.Cancelled {
			sum += a.Amount
		}
	}

	return sum
}

// Unallocated is the part of the amount applied to no payable (an advance).
func (v *PaymentOutVoucher) Unallocated() int64 { return v.Amount - v.Allocated() }
