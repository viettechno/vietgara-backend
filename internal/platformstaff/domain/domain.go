// Package domain holds the platform staff model of Release 8.1 (FR-IAM-10):
// roles (sets of platform permissions), staff groups that hold a role,
// their members and the invitations that make a user a member. It mirrors
// the garage roles and staff groups of the Tenant module in its own scope,
// which has no garage.
package domain

import (
	"errors"
	"time"
	"unicode/utf8"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// Name limits.
const (
	MaxRoleNameLength  = 100
	MaxGroupNameLength = 100
	MaxDescriptionSize = 500
)

// InvitationTTL is how long an invitation can be accepted.
const InvitationTTL = 7 * 24 * time.Hour

// ValidName reports whether name can name a role or a group.
func ValidName(name string, maxLength int) bool {
	return name != "" && utf8.RuneCountInString(name) <= maxLength
}

// Role is a named set of platform permissions.
type Role struct {
	ID          string
	Name        string
	Description string
	Permissions []access.PlatformPermission
	// GroupCount is the number of groups using the role (read only).
	GroupCount int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Group is a set of users that all receive the permissions of its role.
type Group struct {
	ID          string
	Name        string
	Description string
	RoleID      string
	// Read only.
	RoleName    string
	MemberCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GroupRef names a group a member belongs to.
type GroupRef struct {
	ID   string
	Name string
}

// Member is a user with platform access: a Platform Admin, or a member of
// at least one group. Name and e-mail come from the user directory.
type Member struct {
	UserID   string
	FullName string
	Email    string
	Admin    bool
	Groups   []GroupRef
}

// InvitationStatus is the lifecycle of an invitation.
type InvitationStatus string

// Valid InvitationStatus values (the labels of the invitation_status enum).
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

// Invitation asks an existing user to join a platform staff group.
type Invitation struct {
	ID          string
	GroupID     string
	UserID      string
	Status      InvitationStatus
	InvitedBy   string
	ExpiresAt   time.Time
	RespondedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Read-only details for display.
	GroupName     string
	RoleName      string
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

// Reasons reported to API clients (google.rpc.ErrorInfo.reason).
const (
	ReasonRoleNameTaken        = "PLATFORM_ROLE_NAME_TAKEN"
	ReasonRoleInUse            = "PLATFORM_ROLE_IN_USE"
	ReasonGroupNameTaken       = "PLATFORM_GROUP_NAME_TAKEN"
	ReasonUserNotFound         = "USER_NOT_FOUND"
	ReasonUserInactive         = "USER_INACTIVE"
	ReasonAlreadyMember        = "ALREADY_MEMBER"
	ReasonAlreadyInvited       = "ALREADY_INVITED"
	ReasonAlreadyAdmin         = "ALREADY_PLATFORM_ADMIN"
	ReasonCannotInviteSelf     = "CANNOT_INVITE_SELF"
	ReasonInvitationNotPending = "INVITATION_NOT_PENDING"
	ReasonInvitationExpired    = "INVITATION_EXPIRED"
	ReasonPermissionNotHeld    = "PERMISSION_NOT_HELD"
)

// Errors reported by the module; the messages reach the API response.
var (
	ErrRoleNotFound         = errors.New("platform role not found")
	ErrGroupNotFound        = errors.New("platform staff group not found")
	ErrInvitationNotFound   = errors.New("invitation not found")
	ErrMemberNotFound       = errors.New("user is not a member of this group")
	ErrUserNotFound         = errors.New("no user with this e-mail address")
	ErrUserInactive         = errors.New("this user is not active")
	ErrRoleNameTaken        = errors.New("a platform role with this name already exists")
	ErrRoleInUse            = errors.New("a platform staff group uses this role")
	ErrGroupNameTaken       = errors.New("a platform staff group with this name already exists")
	ErrAlreadyMember        = errors.New("the user is already a member of this group")
	ErrAlreadyInvited       = errors.New("the user already has a pending invitation to this group")
	ErrAlreadyAdmin         = errors.New("the user is a platform admin and already holds every permission")
	ErrCannotInviteSelf     = errors.New("you cannot invite yourself")
	ErrInvitationNotPending = errors.New("the invitation is no longer pending")
	ErrInvitationExpired    = errors.New("the invitation has expired")
	ErrPermissionNotHeld    = errors.New("you can only give permissions you hold yourself")
)
