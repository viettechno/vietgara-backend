package domain

import "time"

// InvitationTTL is how long an invitation can be accepted (FR-TEN-04).
const InvitationTTL = 7 * 24 * time.Hour

// InvitationStatus is the lifecycle of an invitation.
type InvitationStatus string

// Valid InvitationStatus values.
const (
	InvitationPending  InvitationStatus = "pending"
	InvitationAccepted InvitationStatus = "accepted"
	InvitationDeclined InvitationStatus = "declined"
	InvitationRevoked  InvitationStatus = "revoked"
	InvitationExpired  InvitationStatus = "expired"
)

// IsValid reports whether s is a defined status.
func (s InvitationStatus) IsValid() bool {
	switch s {
	case InvitationPending, InvitationAccepted, InvitationDeclined, InvitationRevoked, InvitationExpired:
		return true
	}

	return false
}

// Invitation asks an existing user to join a garage.
type Invitation struct {
	ID          string
	GarageID    string
	UserID      string
	GroupIDs    []string
	Status      InvitationStatus
	InvitedBy   string
	ExpiresAt   time.Time
	RespondedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Read-only details for display.
	GarageName    string
	FullName      string
	Email         string
	InvitedByName string
}

// EffectiveStatus is Status, with pending invitations past their expiry
// reported as expired.
func (i *Invitation) EffectiveStatus(now time.Time) InvitationStatus {
	if i.Status == InvitationPending && !now.Before(i.ExpiresAt) {
		return InvitationExpired
	}

	return i.Status
}

// respond moves a live pending invitation to status at now.
func (i *Invitation) respond(status InvitationStatus, now time.Time) error {
	switch i.EffectiveStatus(now) {
	case InvitationPending:
	case InvitationExpired:
		return ErrInvitationExpired
	case InvitationAccepted, InvitationDeclined, InvitationRevoked:
		return ErrInvitationNotPending
	default:
		return ErrInvitationNotPending
	}

	i.Status = status
	i.RespondedAt = &now

	return nil
}

// Accept marks the invitation accepted.
func (i *Invitation) Accept(now time.Time) error { return i.respond(InvitationAccepted, now) }

// Decline marks the invitation declined.
func (i *Invitation) Decline(now time.Time) error { return i.respond(InvitationDeclined, now) }

// Revoke cancels the invitation.
func (i *Invitation) Revoke(now time.Time) error { return i.respond(InvitationRevoked, now) }
