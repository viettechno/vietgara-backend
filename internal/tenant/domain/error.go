package domain

import "errors"

// Sentinel errors for business-rule violations in the Tenant/Garage module.
var (
	ErrGarageNotFound     = errors.New("garage not found")
	ErrGarageNameRequired = errors.New("garage name is required")
	ErrInvalidRole        = errors.New("invalid staff role")
	ErrStaffAlreadyExists = errors.New("this account is already a staff member of this garage")
	ErrNoAccessToGarage   = errors.New("account has no access to this garage")
)
