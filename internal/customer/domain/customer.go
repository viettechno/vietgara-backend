// Package domain contains pure business types for the Customer & Vehicle
// module (HLD module #4). See Coding_Standards.docx Section 2.1.
package domain

import (
	"fmt"
	"time"
	"unicode/utf8"
)

// Field length limits shared by the module's use cases; they mirror the
// column sizes in db/migrations.
const (
	MaxNameLen    = 255
	MaxAddressLen = 500
	MaxNoteLen    = 2000
	MaxTaxCodeLen = 20
	MaxMakeLen    = 100
	MaxCodeLen    = 50
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
	// IsActive is false once the customer was deactivated instead of
	// deleted because documents refer to it (FR-CAT-07).
	IsActive bool
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
