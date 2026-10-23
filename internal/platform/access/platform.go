package access

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// PlatformPermission is one entry of the platform permission catalogue
// (FR-IAM-10), named "<module>.<action>" like the garage catalogue and
// evaluated by the same engine, but in its own scope: there is no garage.
// The values are the labels of the Postgres "platform_permission" enum.
type PlatformPermission string

// The platform permission catalogue. A Platform Admin holds every
// permission; admin staff hold the union of the roles of their groups.
const (
	PlatformPlanRead              PlatformPermission = "plan.read"
	PlatformPlanWrite             PlatformPermission = "plan.write"
	PlatformSubscriptionRead      PlatformPermission = "subscription.read"
	PlatformSubscriptionWrite     PlatformPermission = "subscription.write"
	PlatformStatsRead             PlatformPermission = "stats.read"
	PlatformGarageDirectoryRead   PlatformPermission = "garage_directory.read"
	PlatformUserRead              PlatformPermission = "user.read"
	PlatformStaffRead             PlatformPermission = "platform_staff.read"
	PlatformStaffWrite            PlatformPermission = "platform_staff.write"
	PlatformSupportRead           PlatformPermission = "support.read"
	PlatformSupportWrite          PlatformPermission = "support.write"
	PlatformAuditRead             PlatformPermission = "audit.read"
	platformPermissionUnspecified PlatformPermission = ""
)

// PlatformCatalogEntry is one platform permission and its module.
type PlatformCatalogEntry struct {
	Permission PlatformPermission
	Module     string
}

// PlatformCatalog lists every platform permission in display order.
var PlatformCatalog = []PlatformCatalogEntry{
	{PlatformPlanRead, "plan"},
	{PlatformPlanWrite, "plan"},
	{PlatformSubscriptionRead, "subscription"},
	{PlatformSubscriptionWrite, "subscription"},
	{PlatformStatsRead, "stats"},
	{PlatformGarageDirectoryRead, "garage_directory"},
	{PlatformUserRead, "user"},
	{PlatformStaffRead, "platform_staff"},
	{PlatformStaffWrite, "platform_staff"},
	{PlatformSupportRead, "support"},
	{PlatformSupportWrite, "support"},
	{PlatformAuditRead, "audit"},
}

// Action returns the part after the module: "read" or "write".
func (p PlatformPermission) Action() string {
	_, action, _ := strings.Cut(string(p), ".")

	return action
}

// Module returns the part before the action.
func (p PlatformPermission) Module() string {
	module, _, _ := strings.Cut(string(p), ".")

	return module
}

// IsValid reports whether p is in the catalogue.
func (p PlatformPermission) IsValid() bool {
	return slices.ContainsFunc(PlatformCatalog, func(e PlatformCatalogEntry) bool { return e.Permission == p })
}

// AllPlatform returns every platform permission.
func AllPlatform() []PlatformPermission {
	all := make([]PlatformPermission, 0, len(PlatformCatalog))
	for _, e := range PlatformCatalog {
		all = append(all, e.Permission)
	}

	return all
}

// NormalizePlatform sorts and de-duplicates permissions in catalogue order,
// drops unknown ones and adds the read permission every write permission
// includes.
func NormalizePlatform(perms []PlatformPermission) []PlatformPermission {
	catalog := make([]string, len(PlatformCatalog))
	for i, e := range PlatformCatalog {
		catalog[i] = string(e.Permission)
	}

	return normalizeNames(catalog, perms)
}

// ErrPlatformDenied is reported when the caller lacks a platform permission;
// the message reaches the API response.
var ErrPlatformDenied = errors.New("your platform role does not allow this action")

// PlatformAccess is what one user may do on the platform.
type PlatformAccess struct {
	UserID string
	// Admin is a Platform Admin (listed in platform_admins): every
	// permission, no approval needed.
	Admin       bool
	Permissions []PlatformPermission
}

// Has reports whether the user holds p (a Platform Admin holds everything).
func (a *PlatformAccess) Has(p PlatformPermission) bool {
	return a.Admin || slices.Contains(a.Permissions, p)
}

// Effective returns the permissions the user holds, a Platform Admin
// getting the whole catalogue.
func (a *PlatformAccess) Effective() []PlatformPermission {
	if a.Admin {
		return AllPlatform()
	}

	return NormalizePlatform(a.Permissions)
}

// Any reports whether the user has any platform access at all.
func (a *PlatformAccess) Any() bool { return a.Admin || len(a.Permissions) > 0 }

// Check reports why a is not allowed to use an endpoint that requires p.
func (a *PlatformAccess) Check(p PlatformPermission) error {
	if p != platformPermissionUnspecified && !a.Has(p) {
		return ErrPlatformDenied
	}

	return nil
}

// PlatformReader loads a user's platform access. Implemented by the
// platform staff module, on every request, so removing a user from a group
// applies at once (ADR-007).
type PlatformReader interface {
	PlatformAccess(ctx context.Context, userID string) (*PlatformAccess, error)
}

type platformAccessKey struct{}

// WithPlatformAccess stores the caller's platform access in ctx.
func WithPlatformAccess(ctx context.Context, a *PlatformAccess) context.Context {
	return context.WithValue(ctx, platformAccessKey{}, a)
}

// PlatformFromContext returns the caller's platform access, set once the
// request passed the platform check.
func PlatformFromContext(ctx context.Context) (*PlatformAccess, bool) {
	a, ok := ctx.Value(platformAccessKey{}).(*PlatformAccess)

	return a, ok
}
