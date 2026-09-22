package domain

import "errors"

// Sentinel errors for business-rule violations in the Tenant/Garage module.
var (
	ErrGarageNotFound     = errors.New("garage not found")
	ErrGarageNameRequired = errors.New("garage name is required")
	ErrInvalidRole        = errors.New("invalid staff role")
	ErrStaffAlreadyExists = errors.New("this account is already a staff member of this garage")
	ErrAccountNotFound    = errors.New("no account found with that email or phone number")
	ErrAccountInactive    = errors.New("this account is not active")
	ErrNoAccessToGarage   = errors.New("account has no access to this garage")
)
