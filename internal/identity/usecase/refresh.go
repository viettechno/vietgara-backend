package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Refresh exchanges a still-valid refresh token for a fresh session
// (FR-IAM-02). The old refresh token is rotated: revoked and replaced, so
// a stolen token has a limited window of reuse. The new access token
// reflects the account's current verification and admin flags.
func (uc *IdentityUseCase) Refresh(ctx context.Context, refreshToken string) (*Session, error) {
	accountID, err := uc.sessions.RefreshTokenAccount(ctx, refreshToken)
	if err != nil {
		return nil, refreshError(err)
	}

	acc, err := uc.findAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	if err := uc.sessions.RevokeRefreshToken(ctx, refreshToken); err != nil {
		return nil, apperror.Internal("revoke old refresh token", err)
	}

	return uc.issueSession(ctx, acc)
}

// Logout revokes the presented refresh token (FR-IAM-02).
func (uc *IdentityUseCase) Logout(ctx context.Context, refreshToken string) error {
	if err := uc.sessions.RevokeRefreshToken(ctx, refreshToken); err != nil {
		return apperror.Internal("revoke refresh token", err)
	}

	return nil
}

func refreshError(err error) error {
	if errors.Is(err, domain.ErrRefreshTokenInvalid) {
		return apperror.Wrap(apperror.CodeUnauthenticated, err.Error(), err).WithReason(domain.ReasonRefreshTokenInvalid)
	}

	return apperror.Internal("read refresh token", err)
}
