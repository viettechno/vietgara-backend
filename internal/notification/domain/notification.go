// Package domain contains pure business types for the Notification module
// (HLD module #14): Phase 1 ships the e-mail channel only (FR-NOT-04);
// SMS (FR-NOT-01), Zalo ZNS (FR-NOT-02) and push (FR-NOT-03) are later
// phases. See Coding_Standards.docx Section 2.1.
package domain

import "time"

// Channel is how a notification was delivered.
type Channel string

// ChannelEmail is the only channel Phase 1 implements.
const ChannelEmail Channel = "email"

// Status is the outcome of one delivery attempt.
type Status string

// Valid Status values.
const (
	StatusSent   Status = "sent"
	StatusFailed Status = "failed"
	// StatusSkipped records an e-mail that was not sent, with a SkipReason.
	StatusSkipped Status = "skipped"
)

// SkipReason says why an e-mail was not sent.
type SkipReason string

// Valid SkipReason values.
const (
	// SkipDisabled: the garage switched the type off.
	SkipDisabled SkipReason = "disabled"
	// SkipOptedOut: the customer opted out of non-essential e-mails.
	SkipOptedOut SkipReason = "opted_out"
	// SkipSuppressed: the address bounced for good or complained.
	SkipSuppressed SkipReason = "suppressed"
	// SkipNoEmail: the customer has no e-mail address.
	SkipNoEmail SkipReason = "no_email"
)

// Log is one append-only record of a notification actually sent, for
// visibility/debugging -- not a dedup mechanism: the Outbox Pattern's
// processed_events already gives the triggering event exactly-once
// delivery (Event Design Section 6).
type Log struct {
	ID      string
	UserID  string
	Channel Channel
	// Template names the rendered e-mail (mail.TemplateRegistrationConfirmation, etc.).
	Template  string
	Recipient string
	Status    Status
	Error     string
	CreatedAt time.Time

	// The fields below are set for the e-mails of a garage (Release 10.1);
	// the account and licence e-mails leave them empty.
	GarageID   string
	CustomerID string
	Type       Type
	// EntityType and EntityID name what the e-mail is about, say "quote" and
	// the quote's id.
	EntityType string
	EntityID   string
	EventID    string
	SkipReason SkipReason
}
