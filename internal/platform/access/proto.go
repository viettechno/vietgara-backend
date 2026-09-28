package access

import (
	"strings"

	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
)

// ToProto maps p to its API enum value: "customer.read" is
// PERMISSION_CUSTOMER_READ.
func ToProto(p Permission) tenantv1.Permission {
	name := "PERMISSION_" + strings.ToUpper(strings.ReplaceAll(string(p), ".", "_"))

	return tenantv1.Permission(tenantv1.Permission_value[name])
}

// FromProto maps an API enum value back to a catalog permission; unknown
// values yield false.
func FromProto(p tenantv1.Permission) (Permission, bool) {
	name := strings.TrimPrefix(p.String(), "PERMISSION_")
	for _, e := range Catalog {
		if strings.ToUpper(strings.ReplaceAll(string(e.Permission), ".", "_")) == name {
			return e.Permission, true
		}
	}

	return permissionUnknown, false
}

// ListToProto maps a permission list to API enum values.
func ListToProto(perms []Permission) []tenantv1.Permission {
	out := make([]tenantv1.Permission, 0, len(perms))
	for _, p := range perms {
		out = append(out, ToProto(p))
	}

	return out
}
