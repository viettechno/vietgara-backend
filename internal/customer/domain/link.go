package domain

import (
	"errors"
	"time"
)

// LinkStatus is the state of the link between a customer profile and a
// VietGara user (FR-CUS-06).
type LinkStatus string

// Valid LinkStatus values.
const (
	// LinkRequested: the garage asked; the user has not answered yet.
	LinkRequested LinkStatus = "requested"
	LinkLinked    LinkStatus = "linked"
	LinkDeclined  LinkStatus = "declined"
	// LinkUnlinked: ended by either side.
	LinkUnlinked LinkStatus = "unlinked"
)

// Open reports whether the link is current: asked or in force.
func (s LinkStatus) Open() bool { return s == LinkRequested || s == LinkLinked }

// LinkChannel is how a link was confirmed.
type LinkChannel string

// Valid LinkChannel values.
const (
	// ChannelOTP: the user told the garage a one-time code.
	ChannelOTP LinkChannel = "otp"
	// ChannelApp: the user accepted in the Customer App.
	ChannelApp LinkChannel = "app"
	// ChannelAutoEmail: a verified user e-mail matched the profile.
	ChannelAutoEmail LinkChannel = "auto_email"
)

// UserLink is the link between one customer profile and a user. The
// garage's side never exposes UserID.
type UserLink struct {
	ID          string
	GarageID    string
	CustomerID  string
	UserID      string
	Status      LinkStatus
	Channel     LinkChannel
	RequestedAt time.Time
	LinkedAt    *time.Time
	EndedAt     *time.Time
}

// Sentinel errors for the customer-user link.
var (
	ErrLinkNotFound = errors.New("link not found")
	// ErrNoMatchingUser is returned when no verified user owns the
	// customer's e-mail, so there is nobody to ask.
	ErrNoMatchingUser = errors.New("no VietGara user matches the customer's verified e-mail")
	// ErrAlreadyLinked is returned when the customer is already linked.
	ErrAlreadyLinked = errors.New("this customer is already linked to a user")
	// ErrNoPendingRequest is returned when a code is confirmed without a
	// request waiting for it.
	ErrNoPendingRequest = errors.New("there is no link request waiting for a code")
	// ErrLinkNotOpen is returned when a link is answered in a state that
	// does not allow it (accepting one already answered, say).
	ErrLinkNotOpen = errors.New("this link request was already answered")
)
