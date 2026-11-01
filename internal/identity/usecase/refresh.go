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
// reflects the user's current verification and admin flags.
func (uc *IdentityUseCase) Refresh(ctx context.Context, refreshToken string) (*Session, error) {
	// The token is taken, not read and then revoked: two refreshes of one
	// token at the same moment would otherwise both pass the read.
	userID, err := uc.sessions.ConsumeRefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, refreshError(err)
	}

	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	return uc.issueSession(ctx, user)
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
