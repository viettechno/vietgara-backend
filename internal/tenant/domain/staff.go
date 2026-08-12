package domain

import "time"

// Role is a staff member's role within one specific garage.
// See: FRD FR-IAM-03, Security Architecture Section 3.1.
type Role string

// Valid Role values within a garage.
const (
	RoleOwner          Role = "owner"
	RoleAccountant     Role = "accountant"
	RoleTechnician     Role = "technician"
	RoleServiceAdvisor Role = "service_advisor"
	RoleInventoryStaff Role = "inventory_staff"
)

// IsValid reports whether r is one of the defined Role values.
func (r Role) IsValid() bool {
	switch r {
	case RoleOwner, RoleAccountant, RoleTechnician, RoleServiceAdvisor, RoleInventoryStaff:
		return true
	}

	return false
}

// StaffStatus is the lifecycle state of a staff membership within a garage.
type StaffStatus string

// Valid StaffStatus values.
const (
	StaffStatusActive    StaffStatus = "active"
	StaffStatusInactive  StaffStatus = "inactive"
	StaffStatusSuspended StaffStatus = "suspended"
)

// GarageStaff assigns an Account a Role within exactly one Garage.
type GarageStaff struct {
	ID        string
	GarageID  string
	AccountID string
	Role      Role
	Status    StaffStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}
