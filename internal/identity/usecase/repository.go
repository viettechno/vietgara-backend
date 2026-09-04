package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
)

// GarageRole is a single (garage_id, role) pair from
// ListGarageRolesForAccount.
type GarageRole struct {
	GarageID string
	Role     string
}

// AccountRepository is implemented by
// internal/identity/repository.PostgresAccountRepository.
type AccountRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Account, error)
	FindByEmail(ctx context.Context, email string) (*domain.Account, error)
	FindByPhone(ctx context.Context, phone string) (*domain.Account, error)
	FindByIdentityPlatformUID(ctx context.Context, uid string) (*domain.Account, error)
	Create(ctx context.Context, acc *domain.Account) error
	UpdatePasswordHash(ctx context.Context, accountID, hash string) error
}

// GarageMembershipReader is a narrow read-only view into the Tenant
// module's data, needed by the Identity module to build the "garages"
// JWT claim at login time. It is implemented by the Tenant module (see
// cmd/server/wiring.go for the adapter), but depended upon here only via
// this interface to keep the dependency direction explicit for when the
// modules are split into separate services later.
type GarageMembershipReader interface {
	// ListGarageRolesForAccount returns every (garage_id, role) pair the
	// given account currently has active staff membership in.
	ListGarageRolesForAccount(ctx context.Context, accountID string) ([]GarageRole, error)
}

// RefreshTokenStore persists refresh tokens (Redis in production) so they
// can be revoked on logout. See Security Architecture Section 2.2.
type RefreshTokenStore interface {
	Store(ctx context.Context, refreshToken, accountID string, ttlSeconds int) error
	GetAccountID(ctx context.Context, refreshToken string) (string, error)
	Revoke(ctx context.Context, refreshToken string) error
}

// OTPVerifier verifies a phone-OTP challenge. The concrete implementation
// (SMS provider backed) is injected at the composition root -- see
// cmd/server/wiring.go -- so the same OTP mechanism can be reused by other
// flows later.
type OTPVerifier interface {
	Verify(ctx context.Context, phone, code string) (bool, error)
}
