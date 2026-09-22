package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// GarageRepository defines the persistence contract for garages.
type GarageRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Garage, error)
	ListByAccountID(ctx context.Context, accountID string) ([]*domain.Garage, error)
	Create(ctx context.Context, g *domain.Garage) error
	Update(ctx context.Context, g *domain.Garage) error
}

// GarageStaffRepository defines the persistence contract for garage staff
// memberships.
type GarageStaffRepository interface {
	Create(ctx context.Context, s *domain.GarageStaff) error
	// FindByID returns the membership with the given id, or pgx.ErrNoRows
	// when absent.
	FindByID(ctx context.Context, staffID string) (*domain.GarageStaff, error)
	// Update persists role/status changes of an existing membership.
	Update(ctx context.Context, s *domain.GarageStaff) error
	ListByGarageID(ctx context.Context, garageID string) ([]*domain.GarageStaff, error)
	FindByGarageAndAccount(ctx context.Context, garageID, accountID string) (*domain.GarageStaff, error)
	// ListRolesForAccount returns every (garage_id, role) pair for an
	// account's active staff memberships -- this is the same data exposed
	// to the Identity module via the GarageMembershipReader interface it
	// depends on (see internal/identity/repository.GarageMembershipReader).
	ListRolesForAccount(ctx context.Context, accountID string) ([]RoleAssignment, error)
}

// RoleAssignment is a single (garage_id, role) pair from
// ListRolesForAccount.
type RoleAssignment struct {
	GarageID string
	Role     string
}

// AccountSummary is the slice of an Identity account the Tenant module needs
// to show and invite staff.
type AccountSummary struct {
	ID       string
	FullName string
	Email    *string
	Phone    *string
	Active   bool
}

// AccountDirectory is a narrow read-only view into the Identity module's
// accounts, implemented by an adapter in cmd/server/wiring.go so the two
// modules stay decoupled. Lookups return domain.ErrAccountNotFound when no
// account matches.
type AccountDirectory interface {
	// FindByEmail matches the email case-insensitively.
	FindByEmail(ctx context.Context, email string) (*AccountSummary, error)
	// FindByPhone matches ignoring spaces, dashes, dots and parentheses.
	FindByPhone(ctx context.Context, phone string) (*AccountSummary, error)
	// ListByIDs returns the accounts found, keyed by account id.
	ListByIDs(ctx context.Context, ids []string) (map[string]*AccountSummary, error)
}
