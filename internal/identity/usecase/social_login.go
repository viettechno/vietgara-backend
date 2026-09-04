package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// VerifiedSocialProfile is the caller's verified identity returned by the
// Identity Platform verifier before IdentityUseCase.SocialLogin is invoked.
type VerifiedSocialProfile struct {
	IdentityPlatformUID string
	Provider            domain.AuthProvider
	Email               *string
	FullName            string
}

// SocialLogin logs an account in with a Google/Facebook identity
// (FR-IAM-01, FR-IAM-02, ADR-007). The handler layer is responsible for
// verifying the provider's id_token with GCP Identity Platform BEFORE
// calling this usecase -- this usecase only deals with our own account
// lookup/creation once the identity_platform_uid and profile info are
// already verified.
func (uc *IdentityUseCase) SocialLogin(ctx context.Context, profile VerifiedSocialProfile) (*AuthResult, error) {
	acc, err := uc.accountRepo.FindByIdentityPlatformUID(ctx, profile.IdentityPlatformUID)
	if err != nil && !errors.Is(err, domain.ErrAccountNotFound) {
		return nil, apperror.Wrap(apperror.CodeInternal, "find account by identity platform uid", err)
	}

	if acc == nil {
		acc = &domain.Account{
			Email:               profile.Email,
			AuthProvider:        profile.Provider,
			IdentityPlatformUID: &profile.IdentityPlatformUID,
			FullName:            profile.FullName,
			Status:              domain.AccountStatusActive,
		}
		if err := uc.accountRepo.Create(ctx, acc); err != nil {
			return nil, apperror.Wrap(apperror.CodeInternal, "create account from social login", err)
		}
	}

	if acc.Status == domain.AccountStatusSuspended {
		return nil, apperror.Wrap(apperror.CodeFailedPrecondition, "account suspended", domain.ErrAccountSuspended)
	}

	return uc.issueSession(ctx, acc)
}
