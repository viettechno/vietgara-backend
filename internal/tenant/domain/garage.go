// Package domain contains pure business types for the Tenant/Garage
// Management module. See Coding_Standards.docx Section 2.1.
package domain

import (
	"errors"
	"time"
)

// GarageStatus is the lifecycle state of a garage.
type GarageStatus string

// Valid GarageStatus values.
const (
	GarageStatusActive    GarageStatus = "active"
	GarageStatusInactive  GarageStatus = "inactive"
	GarageStatusSuspended GarageStatus = "suspended"
)

// Garage is one tenant: an independent operating/data-isolation unit.
// Per Decision Log DEC-003, an Account may own many Garages (1 account --
// many garages); "branch" is used synonymously with "garage" throughout
// VietGara -- a garage is NOT a sub-unit of another garage.
// See: Database Design Section 2.2, ADR-005.
type Garage struct {
	ID        string
	AccountID string
	Name      string
	TaxCode   *string
	Address   *string
	LogoURL   *string
	Timezone  string
	Currency  string
	Status    GarageStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Sentinel errors for business-rule violations in the Tenant/Garage module.
var (
	ErrGarageNotFound     = errors.New("garage not found")
	ErrGarageNameRequired = errors.New("garage name is required")
	ErrInvalidRole        = errors.New("invalid staff role")
	ErrStaffAlreadyExists = errors.New("this account is already a staff member of this garage")
	ErrNoAccessToGarage   = errors.New("account has no access to this garage")
)
