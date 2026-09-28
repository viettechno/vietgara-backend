package domain

import "errors"

// Sentinel errors for business-rule violations in the Tenant/Garage module.
var (
	ErrGarageNotFound       = errors.New("garage not found")
	ErrStaffNotFound        = errors.New("staff membership not found")
	ErrOwnerMembership      = errors.New("the garage owner cannot be deactivated, removed or grouped")
	ErrAccountNotFound      = errors.New("no account found with that email or phone number")
	ErrAccountInactive      = errors.New("this account is not active")
	ErrAlreadyMember        = errors.New("this account is already a member of the garage")
	ErrAlreadyInvited       = errors.New("this account already has a pending invitation")
	ErrInvitationNotFound   = errors.New("invitation not found")
	ErrInvitationNotPending = errors.New("the invitation was already answered or revoked")
	ErrInvitationExpired    = errors.New("the invitation has expired")
	ErrRoleNotFound         = errors.New("role not found")
	ErrRoleNameTaken        = errors.New("a role with this name already exists")
	ErrRoleInUse            = errors.New("the role is used by staff groups")
	ErrGroupNotFound        = errors.New("staff group not found")
	ErrGroupNameTaken       = errors.New("a staff group with this name already exists in the garage")
	ErrInvalidLogo          = errors.New("the logo must be a PNG, JPEG or WebP image of at most 1 MB")
)

// Client-facing reasons of the errors above.
const (
	ReasonAccountNotFound      = "ACCOUNT_NOT_FOUND"
	ReasonAccountInactive      = "ACCOUNT_INACTIVE"
	ReasonAlreadyMember        = "ALREADY_MEMBER"
	ReasonAlreadyInvited       = "ALREADY_INVITED"
	ReasonOwnerMembership      = "OWNER_MEMBERSHIP"
	ReasonInvitationNotPending = "INVITATION_NOT_PENDING"
	ReasonInvitationExpired    = "INVITATION_EXPIRED"
	ReasonRoleNameTaken        = "ROLE_NAME_TAKEN"
	ReasonRoleInUse            = "ROLE_IN_USE"
	ReasonGroupNameTaken       = "GROUP_NAME_TAKEN"
	ReasonInvalidLogo          = "INVALID_LOGO"
)
