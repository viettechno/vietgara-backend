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
