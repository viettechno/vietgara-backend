// Package access authorizes garage-scoped requests (Security Architecture
// Section 3.2, service layer): the caller must own the garage or be one of
// its active staff, and hold the permission the endpoint requires through
// the roles of their staff groups (FR-IAM-03).
//
// Membership and permissions are read from the database on every request
// rather than from the token, so a deactivated member loses access at once
// and a garage created moments ago works before the token is refreshed.
package access

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
)

// Permission is one entry of the fixed permission catalog, named
// "<module>.<action>" with action "read" or "write". The values are the
// labels of the Postgres "permission" enum.
type Permission string

// The permission catalog. Owners hold every permission; every active member
// may read the garage profile.
const (
	GarageWrite   Permission = "garage.write"
	StaffRead     Permission = "staff.read"
	StaffWrite    Permission = "staff.write"
	CustomerRead  Permission = "customer.read"
	CustomerWrite Permission = "customer.write"
	// CustomerCorrect corrects a locked identity field of a vehicle, or
	// moves it to another customer (FR-CUS-09).
	CustomerCorrect  Permission = "customer.correct"
	SupplierRead     Permission = "supplier.read"
	SupplierWrite    Permission = "supplier.write"
	SubscriptionRead Permission = "subscription.read"
	QuoteRead        Permission = "quote.read"
	QuoteWrite       Permission = "quote.write"
	RepairOrderRead  Permission = "repair_order.read"
	RepairOrderWrite Permission = "repair_order.write"
	InventoryRead    Permission = "inventory.read"
	InventoryWrite   Permission = "inventory.write"
	// CatalogueRead / CatalogueWrite cover the parts, units, packaging,
	// categories, prices and images of the Catalogue module (Release 6.0).
	CatalogueRead     Permission = "catalogue.read"
	CatalogueWrite    Permission = "catalogue.write"
	BillingRead       Permission = "billing.read"
	BillingWrite      Permission = "billing.write"
	ReportRead        Permission = "report.read"
	permissionUnknown Permission = ""
)

// Catalog lists every permission in display order with its module.
var Catalog = []CatalogEntry{
	{GarageWrite, "garage"},
	{StaffRead, "staff"},
	{StaffWrite, "staff"},
	{CustomerRead, "customer"},
	{CustomerWrite, "customer"},
	{CustomerCorrect, "customer"},
	{SupplierRead, "supplier"},
	{SupplierWrite, "supplier"},
	{SubscriptionRead, "subscription"},
	{QuoteRead, "quote"},
	{QuoteWrite, "quote"},
	{RepairOrderRead, "repair_order"},
	{RepairOrderWrite, "repair_order"},
	{InventoryRead, "inventory"},
	{InventoryWrite, "inventory"},
	{CatalogueRead, "catalogue"},
	{CatalogueWrite, "catalogue"},
	{BillingRead, "billing"},
	{BillingWrite, "billing"},
	{ReportRead, "report"},
}

// CatalogEntry is one catalog permission and the module it belongs to.
type CatalogEntry struct {
	Permission Permission
	Module     string
}

// Action returns the part after the module: "read", "write" or "correct".
func (p Permission) Action() string {
	_, action, _ := strings.Cut(string(p), ".")

	return action
}

// IsValid reports whether p is in the catalog.
func (p Permission) IsValid() bool {
	return slices.ContainsFunc(Catalog, func(e CatalogEntry) bool { return e.Permission == p })
}

// All returns every catalog permission.
func All() []Permission {
	all := make([]Permission, 0, len(Catalog))
	for _, e := range Catalog {
		all = append(all, e.Permission)
	}

	return all
}

// Normalize sorts and de-duplicates permissions in catalog order, drops
// unknown ones and adds the read permission every write permission
// includes (when its module has one).
func Normalize(perms []Permission) []Permission {
	order := make(map[Permission]int, len(Catalog))
	for i, e := range Catalog {
		order[e.Permission] = i
	}

	seen := make(map[Permission]bool, len(perms))
	out := make([]Permission, 0, len(perms))
	add := func(p Permission) {
		if _, known := order[p]; known && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	for _, p := range perms {
		add(p)

		if module, action, _ := strings.Cut(string(p), "."); action == "write" || action == "correct" {
			add(Permission(module + ".read"))
		}
	}

	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })

	return out
}

// Errors reported to callers; the messages reach the API response.
var (
	ErrNoMembership = errors.New("user has no access to this garage")
	ErrRoleDenied   = errors.New("your role does not allow this action")
	ErrOwnerOnly    = errors.New("only the garage owner can perform this action")
	ErrGarageClosed = errors.New("this garage is inactive")
)

// Access is what one user may do in one garage.
type Access struct {
	GarageID string
	UserID   string
	// OwnerUserID owns the garage (and its subscription).
	OwnerUserID string
	Owner       bool
	Active      bool // the garage is active
	Permissions []Permission
}

// Has reports whether the user holds p (owners hold everything).
func (a *Access) Has(p Permission) bool {
	return a.Owner || slices.Contains(a.Permissions, p)
}

// Effective returns the permissions the user holds, owners getting the
// full catalog.
func (a *Access) Effective() []Permission {
	if a.Owner {
		return All()
	}

	return Normalize(a.Permissions)
}

// Reader loads a user's access to a garage, returning ErrNoMembership
// when the user is neither the owner nor an active member. Implemented
// by the Tenant module.
type Reader interface {
	GarageAccess(ctx context.Context, garageID, userID string) (*Access, error)
}

// Rule is what an endpoint requires from the caller.
type Rule struct {
	// Permission required in the request's garage; permissionUnknown
	// means "any active member".
	Permission Permission
	// OwnerOnly restricts the endpoint to the garage owner.
	OwnerOnly bool
	// AllowInactive lets members reach an inactive garage (the owner can
	// always read and reactivate it).
	AllowInactive bool
}

// Member requires only an active membership.
func Member() Rule { return Rule{} }

// Requires requires permission p.
func Requires(p Permission) Rule { return Rule{Permission: p} }

// OwnerOnly restricts to the garage owner.
func OwnerOnly() Rule { return Rule{OwnerOnly: true} }

// Check reports why a is not allowed rule, or nil.
func (a *Access) Check(rule Rule) error {
	if !a.Active && !a.Owner && !rule.AllowInactive {
		return ErrGarageClosed
	}

	if rule.OwnerOnly && !a.Owner {
		return ErrOwnerOnly
	}

	if rule.Permission != permissionUnknown && !a.Has(rule.Permission) {
		return ErrRoleDenied
	}

	return nil
}

type accessKey struct{}

// WithAccess stores the caller's garage access in ctx.
func WithAccess(ctx context.Context, a *Access) context.Context {
	return context.WithValue(ctx, accessKey{}, a)
}

// FromContext returns the caller's garage access, set once the request's
// garage was authorized.
func FromContext(ctx context.Context) (*Access, bool) {
	a, ok := ctx.Value(accessKey{}).(*Access)

	return a, ok
}
