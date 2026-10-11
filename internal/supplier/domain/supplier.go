// Package domain contains the pure business types of the Supplier module
// (FR-SUP-01): the garage's directory of parts suppliers. A supplier is
// not a customer. See: Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// Field limits, matching the suppliers table.
const (
	MaxNameLen    = 255
	MaxAddressLen = 500
	MaxNoteLen    = 2000
	MaxTaxCodeLen = 20
	MaxBankLen    = 100
)

// Contact is how the garage reaches a supplier; persisted as the
// suppliers.contact_info JSON document.
type Contact struct {
	ContactPerson *string `json:"contact_person,omitempty"`
	Phone         *string `json:"phone,omitempty"`
	Email         *string `json:"email,omitempty"`
	Address       *string `json:"address,omitempty"`
}

// BankDetails is the account the garage pays a supplier into; persisted as
// the suppliers.bank_details JSON document.
type BankDetails struct {
	BankName      *string `json:"bank_name,omitempty"`
	AccountNumber *string `json:"account_number,omitempty"`
	AccountHolder *string `json:"account_holder,omitempty"`
}

// Supplier is a business that sells parts to the garage (FR-SUP-01). See:
// Database Design Section 3.3.
type Supplier struct {
	ID       string
	GarageID string
	Name     string
	TaxCode  *string
	Contact  Contact
	Bank     BankDetails
	Note     *string
	// IsActive is false once the supplier was deactivated instead of
	// deleted because documents refer to it (FR-CAT-07).
	IsActive  bool
	CreatedBy *string
	UpdatedBy *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FieldTooLongError reports a value longer than its column allows. It
// matches ErrFieldTooLong with errors.Is.
type FieldTooLongError struct {
	Field  string
	MaxLen int
}

func (e *FieldTooLongError) Error() string {
	return fmt.Sprintf("%s must be at most %d characters", e.Field, e.MaxLen)
}

// Is makes errors.Is(err, ErrFieldTooLong) true.
func (e *FieldTooLongError) Is(target error) bool { return target == ErrFieldTooLong }

// CheckLength returns a *FieldTooLongError when value has more than maxLen
// characters.
func CheckLength(field, value string, maxLen int) error {
	if utf8.RuneCountInString(value) > maxLen {
		return &FieldTooLongError{Field: field, MaxLen: maxLen}
	}

	return nil
}

// Sentinel errors for business-rule violations in the Supplier module.
var (
	ErrSupplierNotFound     = errors.New("supplier not found")
	ErrSupplierNameRequired = errors.New("supplier name is required")
	ErrSupplierNameTaken    = errors.New("a supplier with this name already exists")

	ErrFieldTooLong = errors.New("field is too long")
)
