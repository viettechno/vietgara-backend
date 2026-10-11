// Package domain contains pure business types for the Billing & Settlement
// module (HLD module #8). See Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"time"

	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

// SettlementStatus is a settlement's collection status, derived from the
// amount actually paid against the total (FR-BIL-01..03).
type SettlementStatus string

// Valid SettlementStatus values.
const (
	// StatusPending: nothing collected yet.
	StatusPending SettlementStatus = "pending"
	// StatusPartiallyPaid: some, but not all, of the total collected.
	StatusPartiallyPaid SettlementStatus = "partially_paid"
	// StatusPaid: the full total collected.
	StatusPaid SettlementStatus = "paid"
)

// IsValid reports whether s is one of the defined SettlementStatus values.
func (s SettlementStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusPartiallyPaid, StatusPaid:
		return true
	default:
		return false
	}
}

// PaymentMethod is how a payment was actually collected (FR-BIL-03).
type PaymentMethod string

// Valid PaymentMethod values. Online is recorded manually in Release
// 1.4 -- staff sees a bank app or gateway dashboard confirmation and
// records it; automatic confirmation via a payment gateway webhook is
// Release 2.0 (FR-PAY-02).
const (
	MethodCash         PaymentMethod = "cash"
	MethodBankTransfer PaymentMethod = "bank_transfer"
	MethodOnline       PaymentMethod = "online"
)

// IsValid reports whether m is one of the defined PaymentMethod values.
func (m PaymentMethod) IsValid() bool {
	switch m {
	case MethodCash, MethodBankTransfer, MethodOnline:
		return true
	default:
		return false
	}
}

// Payment is one amount recorded as collected against a settlement
// (FR-BIL-03); a settlement may receive several. The customer is the only
// payer. Every payment is a payment voucher (phiếu thu, FR-BIL-05).
type Payment struct {
	ID     string
	Amount int64
	Method PaymentMethod
	Note   string
	// VoucherNumber comes from the garage's voucher numbering template.
	VoucherNumber string
	// PayerName and PayerAddress default to the customer's; Reason to a
	// standard text naming the settlement.
	PayerName    string
	PayerAddress string
	Reason       string
	// CollectorName is the staff member who collected the money.
	CollectorName string
	Issuer        issuer.Issuer
	CreatedBy     *string
	CreatedAt     time.Time
}

// Field limits of a voucher, matching the settlement_payments columns.
const (
	MaxPayerNameLength    = 255
	MaxPayerAddressLength = 500
	MaxReasonLength       = 500
)

// Settlement is the final bill of a closed repair order (FR-BIL-01),
// paid by the customer. See: Database Design Section 6.1.
type Settlement struct {
	ID            string
	GarageID      string
	RepairOrderID string
	// VehicleLicensePlate/CustomerName are denormalized from the repair
	// order at creation, the same pattern repair orders use for quotes --
	// this module never queries Repair Order's tables directly.
	VehicleLicensePlate string
	CustomerName        string
	Number              string
	// TotalAmount is copied from the repair order's total at creation.
	TotalAmount int64
	// Issuer is the garage's legal identity when the settlement was created
	// (FR-TEN-06); the receipt prints it.
	Issuer issuer.Issuer
	// PaidAmount is the running total of Payments.
	PaidAmount int64
	Status     SettlementStatus
	Payments   []Payment
	CreatedBy  *string
	UpdatedBy  *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DeriveStatus recomputes Status from the amounts collected so far
// against the total. A zero-total settlement (nothing to collect) is
// immediately Paid.
func (s *Settlement) DeriveStatus() {
	switch {
	case s.PaidAmount >= s.TotalAmount:
		s.Status = StatusPaid
	case s.PaidAmount <= 0:
		s.Status = StatusPending
	default:
		s.Status = StatusPartiallyPaid
	}
}

// RemainingDue returns how much of the total is still to be collected.
func (s *Settlement) RemainingDue() int64 {
	return s.TotalAmount - s.PaidAmount
}

// Sentinel errors for business-rule violations in the Billing & Settlement module.
var (
	ErrSettlementNotFound = errors.New("settlement not found")
	// ErrAlreadySettled is returned when a repair order already has a
	// settlement (the unique constraint on settlements.repair_order_id).
	ErrAlreadySettled = errors.New("this repair order already has a settlement")
	// ErrRepairOrderNotClosed is returned by CreateSettlement when the
	// repair order is not yet closed (FR-BIL-01).
	ErrRepairOrderNotClosed = errors.New("the repair order must be closed before it can be settled")
	ErrInvalidPaymentMethod = errors.New("payment method is invalid")
	// ErrInvalidVoucher is returned when a voucher's payer, address or
	// reason is too long.
	ErrInvalidVoucher = errors.New("the payer name, address or reason is too long")
	// ErrInvalidPaymentAmount is returned when a payment is not positive,
	// or would collect more than the amount still due.
	ErrInvalidPaymentAmount = errors.New("payment amount must be positive and not exceed the amount still due")
)
