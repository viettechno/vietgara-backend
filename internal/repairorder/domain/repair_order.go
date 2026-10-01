// Package domain contains pure business types for the Repair Order module
// (HLD module #6). Release 1.2 only needs the draft created by Quote's
// ConvertQuote (FR-QUO-04): technician assignment, the Kanban board and
// per-line progress (FR-REP-01..06) are Release 1.3 and will extend these
// types then. See Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"time"
)

// RepairOrderStatus is the lifecycle of a repair order (FR-REP-01..06).
// Only StatusOpen is reachable in Release 1.2; the others are set by the
// Repair Order Service of Release 1.3.
type RepairOrderStatus string

// Valid RepairOrderStatus values.
const (
	StatusOpen       RepairOrderStatus = "open"
	StatusInProgress RepairOrderStatus = "in_progress"
	StatusCompleted  RepairOrderStatus = "completed"
	StatusClosed     RepairOrderStatus = "closed"
)

// IsValid reports whether s is one of the defined RepairOrderStatus values.
func (s RepairOrderStatus) IsValid() bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusCompleted, StatusClosed:
		return true
	default:
		return false
	}
}

// ItemType is a repair order line's kind: labor or a part, carried over
// unchanged from the quote line it came from.
type ItemType string

// Valid ItemType values.
const (
	ItemTypeLabor ItemType = "labor"
	ItemTypePart  ItemType = "part"
)

// Line is one labor or part line of a repair order, copied from an
// approved quote line by ConvertQuote.
type Line struct {
	ID          string
	Position    int
	ItemType    ItemType
	ItemRefID   *string
	Description string
	// Decimal string, e.g. "1" or "1.5", matching the source quote line.
	Quantity  string
	UnitPrice int64
	LineTotal int64
}

// RepairOrder is a draft or in-progress repair job for one vehicle
// (FR-REP-01). See: Database Design Section 4.3.
type RepairOrder struct {
	ID       string
	GarageID string
	// QuoteID is nil for the ad-hoc creation of Release 1.3; always set
	// today (every repair order comes from ConvertQuote).
	QuoteID     *string
	VehicleID   string
	Number      string
	Status      RepairOrderStatus
	TotalAmount int64
	Lines       []Line
	ClosedAt    *time.Time
	CreatedBy   *string
	UpdatedBy   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Sentinel errors for business-rule violations in the Repair Order module.
var (
	// ErrRepairOrderNotFound is returned when a repair order does not
	// exist, or is not visible in the current garage scope.
	ErrRepairOrderNotFound = errors.New("repair order not found")
	// ErrAlreadyConverted is returned when a quote was already converted
	// (the unique constraint on repair_orders.quote_id), guarding the
	// race of two concurrent ConvertQuote calls.
	ErrAlreadyConverted = errors.New("this quote was already converted into a repair order")
)
