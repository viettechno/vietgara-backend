// Package domain holds the support access model of Release 8.2 (FR-IAM-11):
// a grant that lets one admin staff member read one garage, read-only, for a
// limited time, and the log of what was read under it.
package domain

import (
	"errors"
	"time"
)

// Session lengths. A grant lasts DefaultTTL unless the request says
// otherwise, and never more than MaxTTL (7 days).
const (
	MinTTL     = time.Hour
	DefaultTTL = 24 * time.Hour
	MaxTTL     = 7 * 24 * time.Hour
)

// RequestLifetime is how long a request waits for the owner before it
// lapses.
const RequestLifetime = 7 * 24 * time.Hour

// MaxReasonLength is the longest reason or ticket reference.
const MaxReasonLength = 500

// Status is derived from the grant's times, never stored.
type Status string

// The grant statuses.
const (
	StatusPending Status = "pending"
	StatusActive  Status = "active"
	StatusExpired Status = "expired"
	StatusRevoked Status = "revoked"
)

// Errors reported to callers (mapped in the use case).
var (
	ErrGrantNotFound   = errors.New("support grant not found")
	ErrGarageNotFound  = errors.New("garage not found")
	ErrGrantNotPending = errors.New("only a pending support request can be approved")
	ErrGrantNotOpen    = errors.New("the support grant is not pending or active")
	ErrGrantNotActive  = errors.New("the support grant is not active")
	ErrGrantOpen       = errors.New("you already have an open support grant for this garage")
	ErrReasonRequired  = errors.New("a reason is required")
	ErrReasonTooLong   = errors.New("the reason is too long")
	ErrInvalidTTL      = errors.New("the session must last between 1 and 168 hours")
)

// Reasons attached to API errors.
const (
	ReasonGrantNotPending = "GRANT_NOT_PENDING"
	ReasonGrantNotOpen    = "GRANT_NOT_OPEN"
	ReasonGrantNotActive  = "GRANT_NOT_ACTIVE"
	ReasonGrantOpen       = "GRANT_ALREADY_OPEN"
)

// Grant is a support access grant.
type Grant struct {
	ID          string
	GarageID    string
	StaffUserID string
	Reason      string
	TTL         time.Duration
	RequestedAt time.Time
	// ApprovedBy is the owner who approved; empty for a Platform Admin's grant.
	ApprovedBy string
	ApprovedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	// GrantedByPlatformAdmin marks a grant that needed no approval.
	GrantedByPlatformAdmin bool

	// Read only, filled by the repository.
	GarageName   string
	StaffName    string
	StaffEmail   string
	StaffLocale  string
	ApproverName string
}

// NewGrant makes a request. A Platform Admin's grant is approved at once.
func NewGrant(garageID, staffUserID, reason string, ttl time.Duration, platformAdmin bool, now time.Time) *Grant {
	g := &Grant{
		GarageID: garageID, StaffUserID: staffUserID, Reason: reason, TTL: ttl, RequestedAt: now,
		GrantedByPlatformAdmin: platformAdmin,
	}

	if platformAdmin {
		g.start(now)
	}

	return g
}

func (g *Grant) start(now time.Time) {
	expires := now.Add(g.TTL)
	g.ApprovedAt, g.ExpiresAt = &now, &expires
}

// Status is the status of the grant at now.
func (g *Grant) Status(now time.Time) Status {
	switch {
	case g.RevokedAt != nil:
		return StatusRevoked
	case g.ApprovedAt == nil:
		if now.After(g.RequestedAt.Add(RequestLifetime)) {
			return StatusExpired
		}

		return StatusPending
	case !now.Before(*g.ExpiresAt):
		return StatusExpired
	default:
		return StatusActive
	}
}

// Approve starts the session at now: the owner answers a pending request.
func (g *Grant) Approve(ownerID string, now time.Time) error {
	if g.Status(now) != StatusPending {
		return ErrGrantNotPending
	}

	g.ApprovedBy = ownerID
	g.start(now)

	return nil
}

// Revoke ends an active grant, or declines a pending request.
func (g *Grant) Revoke(now time.Time) error {
	if status := g.Status(now); status != StatusPending && status != StatusActive {
		return ErrGrantNotOpen
	}

	g.RevokedAt = &now

	return nil
}

// ValidTTL reports whether d can be the length of a session.
func ValidTTL(d time.Duration) bool { return d >= MinTTL && d <= MaxTTL }

// AccessEvent is one read under a grant.
type AccessEvent struct {
	ID          string
	GrantID     string
	GarageID    string
	StaffUserID string
	StaffName   string
	Method      string
	EntityType  string
	EntityID    string
	CreatedAt   time.Time
}

// DirectoryEntry is a garage's name and owner, nothing else.
type DirectoryEntry struct {
	GarageID   string
	Name       string
	OwnerName  string
	OwnerEmail string
	Active     bool
}

// Garage is what the module needs to know about the garage of a request.
type Garage struct {
	ID          string
	Name        string
	OwnerUserID string
	OwnerName   string
	OwnerEmail  string
	OwnerLocale string
}

// OutboxEvent is the delivery status of an event of the garage.
type OutboxEvent struct {
	ID          string
	EventType   string
	Status      string
	Attempts    int
	LastError   string
	CreatedAt   time.Time
	PublishedAt *time.Time
}

// Notification is one entry of the notification log.
type Notification struct {
	ID        string
	Channel   string
	Template  string
	Recipient string
	Status    string
	Error     string
	CreatedAt time.Time
}

// Page is one page of a list.
type Page struct {
	Page, PageSize int
}

// Offset is the number of rows before the page.
func (p Page) Offset() int { return (p.Page - 1) * p.PageSize }
