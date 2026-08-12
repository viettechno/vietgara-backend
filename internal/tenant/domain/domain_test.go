package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

func TestRoleIsValid(t *testing.T) {
	tests := []struct {
		name string
		role domain.Role
		want bool
	}{
		{name: "owner", role: domain.RoleOwner, want: true},
		{name: "accountant", role: domain.RoleAccountant, want: true},
		{name: "technician", role: domain.RoleTechnician, want: true},
		{name: "service advisor", role: domain.RoleServiceAdvisor, want: true},
		{name: "inventory staff", role: domain.RoleInventoryStaff, want: true},
		{name: "empty", role: domain.Role(""), want: false},
		{name: "unrecognized", role: domain.Role("admin"), want: false},
		{name: "wrong case", role: domain.Role("OWNER"), want: false},
		{name: "trailing whitespace", role: domain.Role("owner "), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.role.IsValid())
		})
	}
}

func TestRoleValues(t *testing.T) {
	tests := []struct {
		name string
		got  domain.Role
		want string
	}{
		{name: "owner", got: domain.RoleOwner, want: "owner"},
		{name: "accountant", got: domain.RoleAccountant, want: "accountant"},
		{name: "technician", got: domain.RoleTechnician, want: "technician"},
		{name: "service advisor", got: domain.RoleServiceAdvisor, want: "service_advisor"},
		{name: "inventory staff", got: domain.RoleInventoryStaff, want: "inventory_staff"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.got))
		})
	}
}

func TestGarageStatusValues(t *testing.T) {
	tests := []struct {
		name string
		got  domain.GarageStatus
		want string
	}{
		{name: "active", got: domain.GarageStatusActive, want: "active"},
		{name: "inactive", got: domain.GarageStatusInactive, want: "inactive"},
		{name: "suspended", got: domain.GarageStatusSuspended, want: "suspended"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.got))
		})
	}
}

func TestStaffStatusValues(t *testing.T) {
	tests := []struct {
		name string
		got  domain.StaffStatus
		want string
	}{
		{name: "active", got: domain.StaffStatusActive, want: "active"},
		{name: "inactive", got: domain.StaffStatusInactive, want: "inactive"},
		{name: "suspended", got: domain.StaffStatusSuspended, want: "suspended"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.got))
		})
	}
}

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "garage not found", err: domain.ErrGarageNotFound, want: "garage not found"},
		{name: "garage name required", err: domain.ErrGarageNameRequired, want: "garage name is required"},
		{name: "invalid role", err: domain.ErrInvalidRole, want: "invalid staff role"},
		{name: "staff already exists", err: domain.ErrStaffAlreadyExists, want: "this account is already a staff member of this garage"},
		{name: "no access to garage", err: domain.ErrNoAccessToGarage, want: "account has no access to this garage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.err)
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}
