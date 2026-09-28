package domain

import (
	"time"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// StaffStatus is the lifecycle state of a staff membership within a garage.
type StaffStatus string

// Valid StaffStatus values.
const (
	StaffStatusActive   StaffStatus = "active"
	StaffStatusInactive StaffStatus = "inactive"
)

// IsValid reports whether s is one of the defined StaffStatus values.
func (s StaffStatus) IsValid() bool {
	return s == StaffStatusActive || s == StaffStatusInactive
}

// Staff is an account's membership of one garage (FR-TEN-04). The garage
// owner also has a membership, flagged Owner.
type Staff struct {
	ID        string
	GarageID  string
	AccountID string
	Status    StaffStatus
	// GroupIDs are the staff groups of the garage the member belongs to.
	GroupIDs []string
	// Owner is true for the garage owner's own membership.
	Owner bool
	// Permissions is the union of the member's groups' role permissions.
	Permissions []access.Permission
	// Account details, loaded for listings.
	FullName  string
	Email     string
	Phone     *string
	CreatedAt time.Time
	UpdatedAt time.Time
}
