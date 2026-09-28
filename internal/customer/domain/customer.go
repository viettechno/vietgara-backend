// Package domain contains pure business types for the Customer & Vehicle
// module (HLD module #4). See Coding_Standards.docx Section 2.1.
package domain

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Field length limits shared by the module's use cases; they mirror the
// column sizes in db/migrations.
const (
	MaxNameLen    = 255
	MaxEmailLen   = 255
	MaxAddressLen = 500
	MaxNoteLen    = 2000
	MaxTaxCodeLen = 20
	MaxMakeLen    = 100
	MaxCodeLen    = 50

	minPhoneDigits = 8
	maxPhoneDigits = 15
)

// ConsentRecord is one personal-data consent decision of a customer
// (Decree 13/2023/ND-CP). Records are append-only.
type ConsentRecord struct {
	Granted    bool      `json:"granted"`
	RecordedAt time.Time `json:"recorded_at"`
	RecordedBy string    `json:"recorded_by,omitempty"`
}

// Customer is a vehicle owner served by one garage (FR-CUS-01).
// See: Database Design Section 3.1.
type Customer struct {
	ID         string
	GarageID   string
	FullName   string
	Phone      *string
	Email      *string
	Address    *string
	ConsentLog []ConsentRecord
	// VehicleCount is derived when reading and never persisted.
	VehicleCount int
	CreatedBy    *string
	UpdatedBy    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// LatestConsent returns the most recent consent decision, or nil when none
// was ever recorded.
func (c *Customer) LatestConsent() *ConsentRecord {
	if len(c.ConsentLog) == 0 {
		return nil
	}

	return &c.ConsentLog[len(c.ConsentLog)-1]
}

// RecordConsent appends a consent decision unless it matches the latest one.
func (c *Customer) RecordConsent(granted bool, at time.Time, by string) {
	if latest := c.LatestConsent(); latest != nil && latest.Granted == granted {
		return
	}

	c.ConsentLog = append(c.ConsentLog, ConsentRecord{Granted: granted, RecordedAt: at, RecordedBy: by})
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
