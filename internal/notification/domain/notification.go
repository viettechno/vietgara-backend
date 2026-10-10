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
}
