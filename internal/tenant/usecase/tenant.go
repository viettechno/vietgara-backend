// Package usecase contains the application use cases of the Tenant/Garage
// Management module: garages (FRD 3.2), staff membership and invitations
// (FR-TEN-04), and owner-defined roles and staff groups (FR-IAM-03).
package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// Deps are the dependencies of TenantUseCase.
type Deps struct {
	Garages     GarageRepository
	Staff       StaffRepository
	Roles       RoleRepository
	Groups      GroupRepository
	Invitations InvitationRepository
	Access      access.Reader
	Usage       UsageReader
	Limits      LimitChecker
	Users       UserDirectory
	Logos       LogoFiles
	Mailer      Mailer
	Events      EventPublisher
	Audit       AuditLogger
	Tx          Transactor
	// AppBaseURL is the web admin URL used in e-mailed links.
	AppBaseURL string
	// Now defaults to time.Now.
	Now func() time.Time
}

// TenantUseCase implements every action of the Tenant module.
type TenantUseCase struct {
	Deps
}

// NewTenantUseCase wires the use case.
func NewTenantUseCase(deps Deps) *TenantUseCase {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &TenantUseCase{Deps: deps}
}

func (uc *TenantUseCase) now() time.Time { return uc.Now().UTC() }

// audit records a change made by the caller.
func (uc *TenantUseCase) audit(ctx context.Context, garageID, action, entityType, entityID string, before, after any) error {
	return uc.Audit.Record(ctx, audit.Entry{
		Actor: middleware.UserID(ctx), GarageID: garageID, Action: action,
		EntityType: entityType, EntityID: entityID, Before: before, After: after, IP: middleware.ClientIP(ctx),
	})
}

// userLocale returns the user's language, Vietnamese by default.
func (uc *TenantUseCase) userLocale(ctx context.Context, userID string) string {
	user, err := uc.Users.FindByID(ctx, userID)
	if err != nil || user.Locale != "en" {
		return "vi"
	}

	return "en"
}

// UserLocaleForTest is a test helper for userLocale.
func (uc *TenantUseCase) UserLocaleForTest(ctx context.Context, userID string) string {
	return uc.userLocale(ctx, userID)
}
