package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// GarageRepository persists garages. Finders return domain.ErrGarageNotFound.
type GarageRepository interface {
	// Create inserts g (its ID is set by the caller) and fills timestamps.
	Create(ctx context.Context, g *domain.Garage) error
	FindByID(ctx context.Context, id string) (*domain.Garage, error)
	// ListForUser returns the garages the user owns (any status) or
	// is an active member of (active garages only), oldest first.
	ListForUser(ctx context.Context, userID string) ([]*domain.Garage, error)
	// Update persists the mutable fields of g.
	Update(ctx context.Context, g *domain.Garage) error
}

// StaffRepository persists garage memberships. Finders return
// domain.ErrStaffNotFound.
type StaffRepository interface {
	Create(ctx context.Context, s *domain.Staff) error
	FindByID(ctx context.Context, garageID, staffID string) (*domain.Staff, error)
	FindByUser(ctx context.Context, garageID, userID string) (*domain.Staff, error)
	// List returns the garage's members with their groups and effective
	// permissions (user details are not loaded).
	List(ctx context.Context, garageID string) ([]*domain.Staff, error)
	UpdateStatus(ctx context.Context, s *domain.Staff) error
	// SetGroups replaces the member's groups.
	SetGroups(ctx context.Context, garageID, staffID string, groupIDs []string) error
	Delete(ctx context.Context, garageID, staffID string) error
}

// RoleRepository persists owner-defined roles. Finders return
// domain.ErrRoleNotFound; a duplicate name yields domain.ErrRoleNameTaken.
type RoleRepository interface {
	ListByOwner(ctx context.Context, ownerUserID string) ([]*domain.Role, error)
	FindByID(ctx context.Context, roleID string) (*domain.Role, error)
	Create(ctx context.Context, r *domain.Role) error
	// CreateDefault inserts a default role unless the owner already has the
	// role with that key, and reports whether it inserted.
	CreateDefault(ctx context.Context, r *domain.Role) (bool, error)
	Update(ctx context.Context, r *domain.Role) error
	// Delete returns domain.ErrRoleInUse while groups use the role.
	Delete(ctx context.Context, roleID string) error
}

// GroupRepository persists staff groups. Finders return
// domain.ErrGroupNotFound; a duplicate name yields domain.ErrGroupNameTaken.
type GroupRepository interface {
	List(ctx context.Context, garageID string) ([]*domain.StaffGroup, error)
	FindByID(ctx context.Context, garageID, groupID string) (*domain.StaffGroup, error)
	Create(ctx context.Context, g *domain.StaffGroup) error
	// CreateDefault inserts a default group unless the garage already has
	// the group with that key.
	CreateDefault(ctx context.Context, g *domain.StaffGroup) (bool, error)
	Update(ctx context.Context, g *domain.StaffGroup) error
	Delete(ctx context.Context, garageID, groupID string) error
}

// InvitationRepository persists staff invitations. Finders return
// domain.ErrInvitationNotFound; a second pending invitation yields
// domain.ErrAlreadyInvited.
type InvitationRepository interface {
	Create(ctx context.Context, inv *domain.Invitation) error
	FindByID(ctx context.Context, id string) (*domain.Invitation, error)
	ListByGarage(ctx context.Context, garageID string) ([]*domain.Invitation, error)
	ListByUser(ctx context.Context, userID string) ([]*domain.Invitation, error)
	// Update persists status and responded_at.
	Update(ctx context.Context, inv *domain.Invitation) error
	// ExpireStale marks the user's pending invitations to the garage
	// that are past their expiry as expired.
	ExpireStale(ctx context.Context, garageID, userID string) error
}

// UsageReader counts what an owner uses of their plan limits across all
// their garages.
type UsageReader interface {
	OwnerUsage(ctx context.Context, ownerUserID string) (garages, staff int, err error)
}

// Limit names a plan limit (License Management).
type Limit string

// Plan limits checked by this module.
const (
	LimitGarages Limit = "max_garages"
	LimitStaff   Limit = "max_staff"
)

// LimitChecker enforces the owner's plan limits; implemented by the
// License module. It returns a PLAN_LIMIT_REACHED error when adding one more
// unit to current would exceed the limit.
type LimitChecker interface {
	CheckLimit(ctx context.Context, ownerUserID string, limit Limit, current int) error
}

// UserSummary is the Identity module's view of a user.
type UserSummary struct {
	ID            string
	FullName      string
	Email         string
	Phone         *string
	Locale        string
	Active        bool
	EmailVerified bool
}

// UserDirectory is a read-only view of Identity users, implemented by
// an adapter in cmd/server so the modules stay decoupled. Finders return
// domain.ErrUserNotFound.
type UserDirectory interface {
	FindByEmail(ctx context.Context, email string) (*UserSummary, error)
	FindByPhone(ctx context.Context, phone string) (*UserSummary, error)
	FindByID(ctx context.Context, id string) (*UserSummary, error)
	ListByIDs(ctx context.Context, ids []string) (map[string]*UserSummary, error)
}

// LogoStore keeps garage logos (internal/platform/storage).
type LogoStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) (string, error)
	Delete(ctx context.Context, key string) error
	KeyFromURL(url string) (string, bool)
}

// Mailer delivers e-mails (internal/platform/mail).
type Mailer interface {
	Send(ctx context.Context, msg mail.Message) error
}

// EventPublisher writes domain events to the outbox in the caller's
// transaction.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, userID string, payload any) error
}

// AuditLogger records audited changes in the caller's transaction.
type AuditLogger interface {
	Record(ctx context.Context, e audit.Entry) error
}

// Transactor runs fn in a scoped transaction (see db.TxManager).
type Transactor interface {
	WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error
}
