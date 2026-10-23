package main

import (
	"context"
	"errors"

	identityDomain "github.com/viettechno/vietgara-backend/internal/identity/domain"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	staffDomain "github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
	staffHandler "github.com/viettechno/vietgara-backend/internal/platformstaff/handler"
	staffRepository "github.com/viettechno/vietgara-backend/internal/platformstaff/repository"
	staffUsecase "github.com/viettechno/vietgara-backend/internal/platformstaff/usecase"
	emailpkg "github.com/viettechno/vietgara-backend/pkg/email"
)

// platformStaffModule is the platform roles and staff groups of Release 8.1.
type platformStaffModule struct {
	repo   *staffRepository.Postgres
	uc     *staffUsecase.UseCase
	server *staffHandler.Server
}

// buildPlatformStaff wires the platform staff module. The repository also
// implements access.PlatformReader, which the request pipeline asks for the
// caller's platform permissions on every platform RPC.
func buildPlatformStaff(repo *staffRepository.Postgres, auditLog *audit.Logger, mailer mail.Sender, users *identityRepo.PostgresUserRepository) *platformStaffModule {
	uc := staffUsecase.New(staffUsecase.Deps{
		Repo: repo, Users: platformUserDirectory{users: users}, Audit: auditLog, Mailer: mailer,
		AdminBaseURL: envOr("ADMIN_APP_BASE_URL", "http://admin.localhost:8000"),
	})

	return &platformStaffModule{repo: repo, uc: uc, server: staffHandler.NewServer(uc)}
}

// platformUserDirectory gives the platform staff module a read-only view of
// Identity users.
type platformUserDirectory struct {
	users *identityRepo.PostgresUserRepository
}

func (a platformUserDirectory) FindByEmail(ctx context.Context, email string) (*staffUsecase.UserSummary, error) {
	normalized, err := emailpkg.Normalize(email)
	if err != nil {
		return nil, staffDomain.ErrUserNotFound
	}

	user, err := a.users.FindByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, identityDomain.ErrUserNotFound) {
			return nil, staffDomain.ErrUserNotFound
		}

		return nil, err
	}

	return toStaffSummary(user), nil
}

func (a platformUserDirectory) ListByIDs(ctx context.Context, ids []string) (map[string]*staffUsecase.UserSummary, error) {
	users, err := a.users.FindByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*staffUsecase.UserSummary, len(users))
	for _, user := range users {
		result[user.ID] = toStaffSummary(user)
	}

	return result, nil
}

func toStaffSummary(user *identityDomain.User) *staffUsecase.UserSummary {
	return &staffUsecase.UserSummary{
		ID: user.ID, FullName: user.FullName, Email: user.Email, Locale: string(user.Locale.OrDefault()),
		Active: user.Status == identityDomain.UserStatusActive, EmailVerified: user.EmailVerified(),
	}
}
