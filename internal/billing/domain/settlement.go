// Package domain contains pure business types for the Billing & Settlement
// module (HLD module #8). See Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"time"
)

// SettlementStatus is a settlement's collection status, derived from the
// amounts actually paid against what was allocated (FR-BIL-01..03).
type SettlementStatus string

// Valid SettlementStatus values.
const (
	// StatusPending: nothing collected yet.
	StatusPending SettlementStatus = "pending"
	// StatusPartiallyPaid: some, but not all, of the allocated amount collected.
	StatusPartiallyPaid SettlementStatus = "partially_paid"
	// StatusPaid: the full allocated amount collected from both payers.
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

// PayerType is who a settlement amount, or a payment against it, is
// attributed to.
type PayerType string

// Valid PayerType values.
const (
	PayerCustomer PayerType = "customer"
	PayerInsurer  PayerType = "insurer"
)

// IsValid reports whether p is one of the defined PayerType values.
func (p PayerType) IsValid() bool { return p == PayerCustomer || p == PayerInsurer }

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
// (FR-BIL-03); a settlement may receive several, from either payer.
type Payment struct {
	ID        string
	PayerType PayerType
	Amount    int64
	Method    PaymentMethod
	Note      string
	CreatedBy *string
	CreatedAt time.Time
}

// Settlement is the final bill of a closed repair order (FR-BIL-01),
// split between what the customer pays directly and what insurance
// covers (FR-BIL-02). See: Database Design Section 6.1.
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
	// CustomerAmount + InsurerAmount always equals TotalAmount (FR-BIL-02).
	// Starts entirely allocated to the customer.
	CustomerAmount int64
	InsurerAmount  int64
	// InsurerPartnerID is set once InsurerAmount > 0; a Partner of type
	// insurer, validated at the application level only (not a foreign
	// key), the same choice repair_order_lines.item_ref_id already makes.
	InsurerPartnerID   *string
	InsurerPartnerName string
	// CustomerPaidAmount/InsurerPaidAmount are running totals of Payments.
	CustomerPaidAmount int64
	InsurerPaidAmount  int64
	Status             SettlementStatus
	Payments           []Payment
	CreatedBy          *string
	UpdatedBy          *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// DeriveStatus recomputes Status from the amounts collected so far
// against the total. A zero-total settlement (nothing to collect) is
// immediately Paid.
func (s *Settlement) DeriveStatus() {
	paid := s.CustomerPaidAmount + s.InsurerPaidAmount

	switch {
	case paid >= s.TotalAmount:
		s.Status = StatusPaid
	case paid <= 0:
		s.Status = StatusPending
	default:
		s.Status = StatusPartiallyPaid
	}
}

// RemainingDue returns how much is still owed by payerType.
func (s *Settlement) RemainingDue(payerType PayerType) int64 {
	if payerType == PayerInsurer {
		return s.InsurerAmount - s.InsurerPaidAmount
	}

	return s.CustomerAmount - s.CustomerPaidAmount
}

// ValidateAllocation checks a proposed customer/insurer split against
// total (FR-BIL-02): both parts must be non-negative, sum to the total,
// and an insurer must be given whenever insurerAmount > 0.
func ValidateAllocation(total, customerAmount, insurerAmount int64, insurerPartnerID *string) error {
	if customerAmount < 0 || insurerAmount < 0 {
		return ErrInvalidAllocation
	}

	if customerAmount+insurerAmount != total {
		return ErrInvalidAllocation
	}

	if insurerAmount > 0 && (insurerPartnerID == nil || *insurerPartnerID == "") {
		return ErrInsurerPartnerRequired
	}

	return nil
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
	// ErrInvalidAllocation is returned when the customer/insurer split
	// does not sum to the settlement's total.
	ErrInvalidAllocation = errors.New("customer amount plus insurer amount must equal the total")
	// ErrInsurerPartnerRequired is returned when insurerAmount > 0 but no
	// insurer partner is given.
	ErrInsurerPartnerRequired = errors.New("an insurer is required when an insurer amount is allocated")
	ErrInsurerNotFound        = errors.New("insurer partner not found")
	ErrInvalidPayerType       = errors.New("payer type must be customer or insurer")
	ErrInvalidPaymentMethod   = errors.New("payment method is invalid")
	// ErrInvalidPaymentAmount is returned when a payment is not positive,
	// or would collect more than the payer's amount still due.
	ErrInvalidPaymentAmount = errors.New("payment amount must be positive and not exceed the amount still due")
)
