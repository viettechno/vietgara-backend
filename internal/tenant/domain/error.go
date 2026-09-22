package domain

import "errors"

// Sentinel errors for business-rule violations in the Tenant/Garage module.
var (
	ErrGarageNotFound      = errors.New("garage not found")
	ErrGarageNameRequired  = errors.New("garage name is required")
	ErrInvalidGarageStatus = errors.New("invalid garage status")
	ErrInvalidRole         = errors.New("invalid staff role")
	ErrInvalidStaffStatus  = errors.New("invalid staff status")
	ErrStaffAlreadyExists  = errors.New("this account is already a staff member of this garage")
	ErrStaffNotFound       = errors.New("staff membership not found")
	ErrOwnerDeactivation   = errors.New("the garage owner cannot be deactivated")
	ErrOwnerRoleChange     = errors.New("the garage owner's role cannot be changed")
	ErrAccountNotFound     = errors.New("no account found with that email or phone number")
	ErrAccountInactive     = errors.New("this account is not active")
	ErrNoAccessToGarage    = errors.New("account has no access to this garage")
)
