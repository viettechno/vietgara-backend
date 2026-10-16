// Package domain contains pure business types for the Quote & Pricing
// module (HLD module #5). See Coding_Standards.docx Section 2.1.
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
	MaxNotesLen       = 2000
	MaxDescriptionLen = 500
	MaxLines          = 100
)

// Status is the lifecycle of a quote (FR-QUO-01..04).
type Status string

// Valid Status values.
const (
	StatusDraft     Status = "draft"
	StatusSent      Status = "sent"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusConverted Status = "converted"
)

// IsValid reports whether s is one of the defined Status values.
func (s Status) IsValid() bool {
	switch s {
	case StatusDraft, StatusSent, StatusApproved, StatusRejected, StatusConverted:
		return true
	default:
		return false
	}
}

// Editable reports whether a quote in status s may still have its fields
// (customer, vehicle, lines, notes, approval mode) changed.
func (s Status) Editable() bool { return s == StatusDraft }

// Sendable reports whether SendQuote may run on a quote in status s: a
// draft being sent for the first time, or a sent quote getting a fresh
// link (e.g. the previous one expired).
func (s Status) Sendable() bool { return s == StatusDraft || s == StatusSent }

// Decidable reports whether a customer decision (via the public link or
// RecordQuoteDecision) may be recorded on a quote in status s.
func (s Status) Decidable() bool { return s == StatusSent }

// ApprovalMode selects how a quote is approved by the customer
// (FR-QUO-02); see Coding_Standards.docx and Security Architecture for the
// LINK/MANUAL distinction (the gara-owner requirement this module adds).
type ApprovalMode string

// Valid ApprovalMode values.
const (
	// ApprovalModeLink sends a public approval link, e-mailed to the
	// customer automatically when they have an e-mail on file.
	ApprovalModeLink ApprovalMode = "link"
	// ApprovalModeManual sends no link: a staff member records the
	// customer's decision directly (e.g. approved face to face).
	ApprovalModeManual ApprovalMode = "manual"
)

// IsValid reports whether m is one of the defined ApprovalMode values.
func (m ApprovalMode) IsValid() bool {
	return m == ApprovalModeLink || m == ApprovalModeManual
}

// ItemType is a quote line's kind: labor or a part.
type ItemType string

// Valid ItemType values.
const (
	ItemTypeLabor ItemType = "labor"
	ItemTypePart  ItemType = "part"
)

// IsValid reports whether t is one of the defined ItemType values.
func (t ItemType) IsValid() bool { return t == ItemTypeLabor || t == ItemTypePart }

// LineDecision is the customer's decision on one line item (FR-QUO-03).
type LineDecision string

// Valid LineDecision values.
const (
	DecisionPending  LineDecision = "pending"
	DecisionApproved LineDecision = "approved"
	DecisionRejected LineDecision = "rejected"
)

// Line is one labor or part line of a quote.
type Line struct {
	ID          string
	Position    int
	ItemType    ItemType
	ItemRefID   *string
	Description string
	// Quantity is a decimal string with at most 3 decimal places, e.g.
	// "1" or "1.5" (ParseQuantity validates and scales it).
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
	Decision         LineDecision
	DecidedAt        *time.Time
}

// Quote is a price estimate awaiting the customer's approval (FR-QUO-01).
// See: Database Design Section 4.1.
type Quote struct {
	ID           string
	GarageID     string
	CustomerID   string
	CustomerName string // derived when reading, never persisted
	VehicleID    string
	// VehicleLicensePlate is derived when reading, never persisted.
	VehicleLicensePlate string
	// Number is rendered from the garage's quote_number_template
	// (FR-TEN-05) when the quote is created.
	Number       string
	Status       Status
	ApprovalMode ApprovalMode
	Lines        []Line
	// TotalAmount is the sum of Lines[i].LineTotal.
	TotalAmount int64
	Notes       string
	// Issuer is the garage's legal identity when the quote was created or
	// sent (FR-TEN-06); its PDF prints it.
	Issuer issuer.Issuer
	// BillingParty is who the quote is billed to (FR-CUS-07), a snapshot of
	// the customer's tax profile; the zero value is a retail receipt.
	BillingParty billingparty.Party
	// PublicTokenHash/PublicTokenExpiresAt back the LINK-mode approval
	// link (FR-QUO-02); the plaintext token is never stored, only its
	// SHA-256 hash. Both are cleared once the quote is decided.
	PublicTokenHash      *string
	PublicTokenExpiresAt *time.Time
	SentAt               *time.Time
	DecidedAt            *time.Time
	ConvertedAt          *time.Time
	CreatedBy            *string
	UpdatedBy            *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// Recalculate sums q.Lines into q.TotalAmount.
func (q *Quote) Recalculate() {
	var total int64
	for _, line := range q.Lines {
		total += line.LineTotal
	}

	q.TotalAmount = total
}

// Sentinel errors for business-rule violations in the Quote & Pricing
// module.
var (
	ErrQuoteNotFound = errors.New("quote not found")

	ErrCustomerRequired    = errors.New("customer is required")
	ErrVehicleRequired     = errors.New("vehicle is required")
	ErrNoLines             = errors.New("a quote needs at least one line")
	ErrTooManyLines        = errors.New("a quote may have at most 100 lines")
	ErrDescriptionRequired = errors.New("line description is required")
	ErrInvalidItemType     = errors.New("item type must be labor or part")
	ErrInvalidQuantity     = errors.New("quantity must be a positive number with at most 3 decimal places")
	ErrInvalidUnitPrice    = errors.New("unit price must not be negative")
	ErrInvalidApprovalMode = errors.New("approval mode must be link or manual")
	ErrFieldTooLong        = errors.New("field is too long")

	// ErrNotEditable is returned when a create/send/decision action is
	// attempted on a quote whose status forbids it (e.g. editing a sent
	// quote, sending an already-sent one).
	ErrNotEditable = errors.New("the quote can no longer be changed in its current status")
	// ErrNotApproved is returned by Convert when the quote is not approved.
	ErrNotApproved = errors.New("only an approved quote can be converted into a repair order")
	// ErrDecisionMismatch is returned when the decisions given do not
	// cover exactly the quote's current lines.
	ErrDecisionMismatch = errors.New("a decision is required for every line, and only for existing lines")
	// ErrLinkModeOnly is returned when a LINK-only action (SendQuote's
	// e-mail/link) is attempted on a MANUAL-mode quote, or the reverse.
	ErrLinkModeOnly = errors.New("this action needs approval_mode LINK")
	// ErrPublicTokenInvalid is returned for an unknown, expired or
	// already-used public approval token.
	ErrPublicTokenInvalid = errors.New("this quote link is invalid or has expired")
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
// decimal places).
const QuantityScale = 1000

// ParseQuantity validates a quote line's quantity and returns it scaled by
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
