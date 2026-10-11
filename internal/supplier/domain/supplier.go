// Package domain contains the pure business types of the Supplier module
// (FR-SUP-01): the garage's directory of parts suppliers. A supplier is
// not a customer. See: Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits, matching the suppliers table.
const (
	MaxNameLen    = 255
	MaxEmailLen   = 255
	MaxAddressLen = 500
	MaxNoteLen    = 2000
	MaxTaxCodeLen = 20
	MaxBankLen    = 100

	minPhoneDigits = 8
	maxPhoneDigits = 15
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
	ID        string
	GarageID  string
	Name      string
	TaxCode   *string
	Contact   Contact
	Bank      BankDetails
	Note      *string
	CreatedBy *string
	UpdatedBy *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

var phoneFormatting = strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "")

// NormalizePhone strips formatting characters and folds the Vietnamese
// "+84" prefix to "0", so "+84 912-345-678" and "0912345678" are the same
// number. It rejects anything that is not 8 to 15 digits.
func NormalizePhone(phone string) (string, error) {
	p := phoneFormatting.Replace(strings.TrimSpace(phone))
	if rest, ok := strings.CutPrefix(p, "+84"); ok {
		p = "0" + rest
	}

	digits := strings.TrimPrefix(p, "+")
	if len(digits) < minPhoneDigits || len(digits) > maxPhoneDigits || strings.Trim(digits, "0123456789") != "" {
		return "", ErrInvalidPhone
	}

	return p, nil
}

// NormalizeEmail trims and lower-cases an email address and checks it is a
// bare address (no display name).
func NormalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if len(e) > MaxEmailLen {
		return "", ErrInvalidEmail
	}

	parsed, err := mail.ParseAddress(e)
	if err != nil || parsed.Address != e {
		return "", ErrInvalidEmail
	}

	return e, nil
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

	ErrInvalidPhone = errors.New("phone number must contain 8 to 15 digits")
	ErrInvalidEmail = errors.New("email address is invalid")
	ErrFieldTooLong = errors.New("field is too long")
)
