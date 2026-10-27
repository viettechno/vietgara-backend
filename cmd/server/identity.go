package main

import (
	"fmt"
	"log/slog"

	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	identityUsecase "github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/secretbox"
)

// identityOptions are the optional parts of the Identity use case. MFA
// (FR-IAM-09) is on when MFA_ENCRYPTION_KEY is set: the TOTP secrets are
// encrypted with it. Without the key the sign-in never asks for a second
// factor and the MFA API answers MFA_NOT_CONFIGURED.
func identityOptions(pool *platformdb.Pool, auditLog *audit.Logger, platformAccess identityUsecase.PlatformAccessChecker) ([]identityUsecase.Option, error) {
	opts := []identityUsecase.Option{
		identityUsecase.WithPlatformAdminEmails(envList("PLATFORM_ADMIN_EMAILS")),
		identityUsecase.WithAudit(auditLog),
		identityUsecase.WithPlatformSignInAudit(platformAccess),
	}

	key := envOr("MFA_ENCRYPTION_KEY", "")
	if key == "" {
		slog.Warn("MFA_ENCRYPTION_KEY is not set: two-factor authentication is unavailable")

		return opts, nil
	}

	box, err := secretbox.New(key)
	if err != nil {
		return nil, fmt.Errorf("MFA_ENCRYPTION_KEY: %w", err)
	}

	return append(opts, identityUsecase.WithMFA(identityRepo.NewPostgresMFARepository(pool), box)), nil
}
