package domain

import (
	"time"
	"unicode/utf8"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

// MaxRoleNameLength and MaxGroupNameLength bound names.
const (
	MaxRoleNameLength  = 100
	MaxGroupNameLength = 100
)

// DefaultRole identifies a role (and group) the platform creates for every
// owner and garage.
type DefaultRole string

// Default roles (FR-IAM-03 roles of the FRD).
const (
	DefaultManager        DefaultRole = "manager"
	DefaultAccountant     DefaultRole = "accountant"
	DefaultServiceAdvisor DefaultRole = "service_advisor"
	DefaultTechnician     DefaultRole = "technician"
	DefaultInventoryStaff DefaultRole = "inventory_staff"
)

// Role is a named permission set defined by a garage owner and shared by
// all the owner's garages.
type Role struct {
	ID          string
	OwnerUserID string
	Name        string
	Description string
	Permissions []access.Permission
	DefaultKey  *DefaultRole
	// GroupCount is the number of staff groups using the role (read only).
	GroupCount int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ValidRoleName reports whether name can name a role or group.
func ValidRoleName(name string, maxLength int) bool {
	return name != "" && utf8.RuneCountInString(name) <= maxLength
}

// DefaultRoleTemplate describes one default role.
type DefaultRoleTemplate struct {
	Key         DefaultRole
	Name        map[string]string // by locale
	Description map[string]string
	Permissions []access.Permission
}

// DefaultRoleTemplates are the roles created when an owner verifies their
// e-mail (and the default staff groups of every new garage).
var DefaultRoleTemplates = []DefaultRoleTemplate{
	{
		Key:  DefaultManager,
		Name: map[string]string{"vi": "Quản lý", "en": "Manager"},
		Description: map[string]string{
			"vi": "Toàn quyền vận hành gara, trừ gói dịch vụ và vai trò.",
			"en": "Runs the garage day to day; cannot change the plan or roles.",
		},
		Permissions: access.All(),
	},
	{
		Key:  DefaultAccountant,
		Name: map[string]string{"vi": "Kế toán", "en": "Accountant"},
		Description: map[string]string{
			"vi": "Quyết toán, công nợ, hoá đơn và báo cáo.",
			"en": "Settlements, receivables, invoices and reports.",
		},
		Permissions: []access.Permission{
			access.CustomerRead, access.PartnerRead, access.SubscriptionRead, access.QuoteRead, access.RepairOrderRead,
			access.InventoryRead, access.BillingRead, access.BillingWrite, access.ReportRead,
		},
	},
	{
		Key:  DefaultServiceAdvisor,
		Name: map[string]string{"vi": "Cố vấn dịch vụ", "en": "Service Advisor"},
		Description: map[string]string{
			"vi": "Tiếp nhận khách hàng, báo giá và lệnh sửa chữa.",
			"en": "Customers, quotes and repair orders.",
		},
		Permissions: []access.Permission{
			access.CustomerRead, access.CustomerWrite, access.PartnerRead, access.QuoteRead, access.QuoteWrite,
			access.RepairOrderRead, access.RepairOrderWrite, access.InventoryRead,
		},
	},
	{
		Key:  DefaultTechnician,
		Name: map[string]string{"vi": "Kỹ thuật viên", "en": "Technician"},
		Description: map[string]string{
			"vi": "Cập nhật tiến độ lệnh sửa chữa được giao.",
			"en": "Updates the progress of assigned repair orders.",
		},
		Permissions: []access.Permission{access.RepairOrderRead, access.RepairOrderWrite, access.InventoryRead},
	},
	{
		Key:  DefaultInventoryStaff,
		Name: map[string]string{"vi": "Nhân viên kho", "en": "Inventory Staff"},
		Description: map[string]string{
			"vi": "Nhập, xuất và tồn kho phụ tùng.",
			"en": "Parts stock in, stock out and stock on hand.",
		},
		Permissions: []access.Permission{
			access.InventoryRead, access.InventoryWrite, access.PartnerRead, access.PartnerWrite, access.RepairOrderRead,
		},
	},
}

// Template returns the template of default role k.
func (k DefaultRole) Template() (DefaultRoleTemplate, bool) {
	for _, t := range DefaultRoleTemplates {
		if t.Key == k {
			return t, true
		}
	}

	return DefaultRoleTemplate{}, false
}

// Localized returns the template's name and description in locale
// (Vietnamese by default).
func (t DefaultRoleTemplate) Localized(locale string) (name, description string) {
	if locale != "en" {
		locale = "vi"
	}

	return t.Name[locale], t.Description[locale]
}

// StaffGroup is a set of members of one garage sharing a role.
type StaffGroup struct {
	ID          string
	GarageID    string
	Name        string
	Description string
	RoleID      string
	// RoleName and MemberCount are read only.
	RoleName    string
	MemberCount int
	DefaultKey  *DefaultRole
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
