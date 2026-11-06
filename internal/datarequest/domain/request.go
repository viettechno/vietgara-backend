// Package domain holds the personal-data requests of customers (FR-CUS-05,
// PD-06): to view, correct or delete the data a garage holds.
package domain

import (
	"errors"
	"time"
)

// Type is what a person asks about their data.
type Type string

// The request types.
const (
	TypeView    Type = "view"
	TypeCorrect Type = "correct"
	TypeDelete  Type = "delete"
)

// IsValid reports whether t is a request type.
func (t Type) IsValid() bool { return t == TypeView || t == TypeCorrect || t == TypeDelete }

// Status is where a request stands.
type Status string

// The request statuses.
const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusRejected  Status = "rejected"
)

// Source is how a request reached the garage.
type Source string

// The request sources.
const (
	SourceStaff  Source = "staff"
	SourcePublic Source = "public"
)

// MaxTextLen bounds the details and the outcome of a request.
const MaxTextLen = 2000

// Request is one request about one customer's data, with the garage's answer.
// It is a record kept as proof of the answer: besides the customer id it holds
// only the text of a correction, which anonymizing the customer clears.
type Request struct {
	ID         string
	GarageID   string
	CustomerID string
	Type       Type
	Status     Status
	Source     Source
	Details    string
	Outcome    string
	// RequestedBy is the staff member who recorded it; nil for a public one.
	RequestedBy *string
	RequestedAt time.Time
	CompletedAt *time.Time
	// CompletedBy is the staff member who completed it; nil when it completed
	// itself. CompletedByName is derived when reading.
	CompletedBy     *string
	CompletedByName string
	// ExportJSON is the data a view answered with; it is set by the creating
	// call only and never stored.
	ExportJSON string
}

// Pending reports whether the request waits for the garage.
func (r *Request) Pending() bool { return r.Status == StatusPending }

// Confirmation is a public request waiting for its e-mail link. The person is
// known only by the address; the token is kept as a hash.
type Confirmation struct {
	ID        string
	Email     string
	Type      Type
	Details   string
	Locale    string
	TokenHash string
	// SourceHash is a keyed hash of where the request came from, for throttling.
	SourceHash string
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

// Errors of a data request.
var (
	ErrRequestNotFound = errors.New("data request not found")
	// ErrNotPending is returned for completing a request that is not waiting.
	ErrNotPending = errors.New("only a pending request can be completed")
	// ErrDetailsRequired is returned for a correction without its text.
	ErrDetailsRequired = errors.New("a correction needs details of what to correct")
	// ErrOutcomeRequired is returned for completing or declining without a note.
	ErrOutcomeRequired = errors.New("an outcome is required")
	ErrTextTooLong     = errors.New("the text is too long")
	ErrInvalidType     = errors.New("the request type must be view, correct or delete")
	ErrInvalidEmail    = errors.New("the e-mail address is not valid")
	// ErrTokenInvalid is returned for a confirmation token that is unknown,
	// used or expired.
	ErrTokenInvalid = errors.New("the confirmation link is not valid")
	// ErrTooManyRequests is returned when an address or a source asked too often.
	ErrTooManyRequests = errors.New("too many requests, try again later")
)

// ReasonNotPending is the error reason of completing a request that is not
// waiting.
const ReasonNotPending = "DATA_REQUEST_NOT_PENDING"
