package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Refresh exchanges a still-valid refresh token for a fresh token pair
// (FR-IAM-02). The old refresh token is rotated: revoked and replaced, so
// a stolen token has a limited window of reuse.
func (uc *IdentityUseCase) Refresh(ctx context.Context, refreshToken string) (*AuthResult, error) {
	accountID, err := uc.tokenStore.GetAccountID(ctx, refreshToken)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeUnauthenticated, "invalid or revoked refresh token", err)
	}

	acc, err := uc.accountRepo.FindByID(ctx, accountID)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeUnauthenticated, "account not found", err)
	}

	if err := uc.tokenStore.Revoke(ctx, refreshToken); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "revoke old refresh token", err)
	}

	return uc.issueSession(ctx, acc)
}

// Logout revokes the presented refresh token (FR-IAM-02).
func (uc *IdentityUseCase) Logout(ctx context.Context, refreshToken string) error {
	if err := uc.tokenStore.Revoke(ctx, refreshToken); err != nil {
		return apperror.Wrap(apperror.CodeInternal, "revoke refresh token", err)
	}

	return nil
}
