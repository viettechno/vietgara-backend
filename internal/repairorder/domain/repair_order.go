// Package domain contains pure business types for the Repair Order module
// (HLD module #6): the draft created by Quote's ConvertQuote (FR-QUO-04,
// Release 1.2) and, from Release 1.3, ad-hoc creation, technician
// assignment, the Kanban board and per-line progress (FR-REP-01..03/06).
// Before/after photos (FR-REP-04) remain Phase 3.
// See Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/viettechno/vietgara-backend/pkg/billingparty"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

// Field length limits shared by the module's use cases.
const (
	MaxDescriptionLen = 500
	MaxLines          = 100
)

// RepairOrderStatus is the lifecycle of a repair order (FR-REP-01..06),
// shown as the Kanban board's columns (UI Design Spec Section 3.3).
type RepairOrderStatus string

// Valid RepairOrderStatus values.
const (
	// StatusOpen is the Kanban board's "To Do" column.
	StatusOpen       RepairOrderStatus = "open"
	StatusInProgress RepairOrderStatus = "in_progress"
	// StatusWaitingOnParts: blocked on a part not currently in stock.
	StatusWaitingOnParts RepairOrderStatus = "waiting_on_parts"
	// StatusCompleted: every line is done; ready for the advisor to close it.
	StatusCompleted RepairOrderStatus = "completed"
	// StatusClosed (FR-REP-06): no further edits; ready for settlement
	// (Release 1.4) and triggers stock deduction (Event Design).
	StatusClosed RepairOrderStatus = "closed"
)

// IsValid reports whether s is one of the defined RepairOrderStatus values.
func (s RepairOrderStatus) IsValid() bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusWaitingOnParts, StatusCompleted, StatusClosed:
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

// IsValid reports whether t is one of the defined ItemType values.
func (t ItemType) IsValid() bool { return t == ItemTypeLabor || t == ItemTypePart }

// LineStatus is one line's own progress (FR-REP-03), tracked separately
// from the repair order's overall status.
type LineStatus string

// Valid LineStatus values.
const (
	LineNotStarted LineStatus = "not_started"
	LineInProgress LineStatus = "in_progress"
	LineCompleted  LineStatus = "completed"
)

// IsValid reports whether s is one of the defined LineStatus values.
func (s LineStatus) IsValid() bool {
	switch s {
	case LineNotStarted, LineInProgress, LineCompleted:
		return true
	default:
		return false
	}
}

// Line is one labor or part line of a repair order.
type Line struct {
	ID          string
	Position    int
	ItemType    ItemType
	ItemRefID   *string
	Description string
	// Decimal string, e.g. "1" or "1.5", matching the source quote line.
	Quantity string
	// UnitID and EnteredQuantity keep the packaging unit the user typed a
	// part line's quantity in (FR-CAT-02); Quantity is then the base-unit
	// amount derived from it. Both are nil for labor and base-unit lines.
	// UnitAbbreviation is derived when reading.
	UnitID           *string
	EnteredQuantity  *string
	UnitAbbreviation string
	UnitPrice        int64
	LineTotal        int64
	Status           LineStatus
	// AssignedTechnicianIDs are the user ids assigned to this line
	// (FR-REP-02); a line may have more than one.
	AssignedTechnicianIDs []string
}

// RepairOrder is a repair job for one vehicle (FR-REP-01), either
// converted from an approved quote or created directly for ad-hoc work.
// See: Database Design Section 4.3.
type RepairOrder struct {
	ID       string
	GarageID string
	// QuoteID is nil for an ad-hoc repair order (FR-REP-01).
	QuoteID   *string
	VehicleID string
	// VehicleLicensePlate/CustomerName are derived when reading, never
	// persisted (denormalized the same way Quote does, so this module
	// never queries Customer's tables directly).
	VehicleLicensePlate string
	CustomerName        string
	Number              string
	Status              RepairOrderStatus
	TotalAmount         int64
	Lines               []Line
	// Issuer is the garage's legal identity when the repair order was
	// created (FR-TEN-06); its PDF prints it.
	Issuer issuer.Issuer
	// BillingParty is who the repair order is billed to (FR-CUS-07): the
	// quote's, which the settlement starts from. Zero is retail.
	BillingParty billingparty.Party
	ClosedAt     *time.Time
	CreatedBy    *string
	UpdatedBy    *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Recalculate sums ro.Lines into ro.TotalAmount.
func (ro *RepairOrder) Recalculate() {
	var total int64
	for _, line := range ro.Lines {
		total += line.LineTotal
	}

	ro.TotalAmount = total
}

// DeriveStatus recomputes the repair order's overall status from its
// lines' own status (FR-REP-03), leaving StatusClosed untouched -- closing
// is only ever set explicitly by Close (FR-REP-06), never derived back
// down from line edits.
func (ro *RepairOrder) DeriveStatus() {
	if ro.Status == StatusClosed {
		return
	}

	allCompleted, anyStarted := true, false

	for _, line := range ro.Lines {
		if line.Status != LineCompleted {
			allCompleted = false
		}

		if line.Status != LineNotStarted {
			anyStarted = true
		}
	}

	switch {
	case len(ro.Lines) > 0 && allCompleted:
		ro.Status = StatusCompleted
	case anyStarted:
		ro.Status = StatusInProgress
	default:
		ro.Status = StatusOpen
	}
}

// ReadyToClose reports whether every line is completed (FR-REP-06).
func (ro *RepairOrder) ReadyToClose() bool {
	if len(ro.Lines) == 0 {
		return false
	}

	for _, line := range ro.Lines {
		if line.Status != LineCompleted {
			return false
		}
	}

	return true
}

// PartLines returns the lines that consume inventory: type PART with a
// resolved item_ref_id (FR-INV-02). A part line left without one (not
// carried in stock) is simply not deducted.
func (ro *RepairOrder) PartLines() []Line {
	var lines []Line

	for _, line := range ro.Lines {
		if line.ItemType == ItemTypePart && line.ItemRefID != nil && *line.ItemRefID != "" {
			lines = append(lines, line)
		}
	}

	return lines
}

// Sentinel errors for business-rule violations in the Repair Order module.
var (
	// ErrRepairOrderNotFound is returned when a repair order does not
	// exist, or is not visible in the current garage scope.
	ErrRepairOrderNotFound = errors.New("repair order not found")
	// ErrLineNotFound is returned when a repair order line does not exist.
	ErrLineNotFound = errors.New("repair order line not found")
	// ErrAlreadyConverted is returned when a quote was already converted
	// (the unique constraint on repair_orders.quote_id), guarding the
	// race of two concurrent ConvertQuote calls.
	ErrAlreadyConverted = errors.New("this quote was already converted into a repair order")

	ErrVehicleRequired     = errors.New("vehicle is required")
	ErrNoLines             = errors.New("a repair order needs at least one line")
	ErrTooManyLines        = errors.New("a repair order may have at most 100 lines")
	ErrDescriptionRequired = errors.New("line description is required")
	ErrInvalidItemType     = errors.New("item type must be labor or part")
	ErrInvalidQuantity     = errors.New("quantity must be a positive number with at most 3 decimal places")
	ErrInvalidUnitPrice    = errors.New("unit price must not be negative")
	ErrInvalidLineStatus   = errors.New("line status is invalid")

	// ErrAlreadyClosed is returned by Close on a repair order that is
	// already closed.
	ErrAlreadyClosed = errors.New("this repair order is already closed")
	// ErrNotReadyToClose is returned by Close while a line is not yet
	// completed (FR-REP-06).
	ErrNotReadyToClose = errors.New("every line must be completed before closing")

	ErrFieldTooLong = errors.New("field is too long")
)

// CheckLength returns ErrFieldTooLong when value has more than maxLen
// characters.
func CheckLength(value string, maxLen int) error {
	if len([]rune(value)) > maxLen {
		return ErrFieldTooLong
	}

	return nil
}

var quantityPattern = regexp.MustCompile(`^\d{1,7}(\.\d{1,3})?$`)

// QuantityScale is the fixed-point precision ParseQuantity scales to (3
// decimal places), matching Quote & Pricing's.
const QuantityScale = 1000

// ParseQuantity validates a line's quantity and returns it scaled by
// QuantityScale (e.g. "1.5" -> 1500), the precision LineTotal computes at.
func ParseQuantity(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if !quantityPattern.MatchString(s) {
		return 0, ErrInvalidQuantity
	}

	intPart, fracPart, _ := strings.Cut(s, ".")
	for len(fracPart) < 3 {
		fracPart += "0"
	}

	scaled, err := strconv.ParseInt(intPart+fracPart, 10, 64)
	if err != nil || scaled == 0 {
		return 0, ErrInvalidQuantity
	}

	return scaled, nil
}

// LineTotal computes a line's total, rounded to the nearest VND, from a
// scaled quantity (ParseQuantity) and a unit price.
func LineTotal(scaledQuantity, unitPrice int64) int64 {
	return (scaledQuantity*unitPrice + QuantityScale/2) / QuantityScale
}
