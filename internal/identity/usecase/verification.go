package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// SendEmailVerificationOTP e-mails a new verification OTP (valid for 2
// minutes) to the signed-in user (FR-IAM-01).
func (uc *IdentityUseCase) SendEmailVerificationOTP(ctx context.Context, userID string) (Challenge, error) {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return Challenge{}, err
	}

	if user.EmailVerified() {
		return Challenge{}, alreadyVerified()
	}

	challenge, err := uc.sendOTP(ctx, domain.OTPEmailVerification, user)
	if err != nil {
		if errors.Is(err, domain.ErrOTPResendTooSoon) {
			return Challenge{}, otpError(err)
		}

		return Challenge{}, apperror.Internal("send verification otp", err)
	}

	return challenge, nil
}

// VerifyEmail confirms the signed-in user's e-mail with the OTP,
// publishes user.email_verified (default roles and trial follow), and
// rotates the session so the new access token is marked verified.
func (uc *IdentityUseCase) VerifyEmail(ctx context.Context, userID, code, refreshToken string) (*Session, error) {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if user.EmailVerified() {
		return nil, alreadyVerified()
	}

	if err := uc.checkOTP(ctx, domain.OTPEmailVerification, user.ID, code); err != nil {
		return nil, err
	}

	if err := uc.markVerified(ctx, user); err != nil {
		return nil, apperror.Internal("mark email verified", err)
	}

	if owner, err := uc.sessions.RefreshTokenUser(ctx, refreshToken); err == nil && owner == user.ID {
		_ = uc.sessions.RevokeRefreshToken(ctx, refreshToken)
	}

	return uc.issueSession(ctx, user)
}

func alreadyVerified() error {
	return apperror.Wrap(apperror.CodeFailedPrecondition, domain.ErrEmailAlreadyVerified.Error(), domain.ErrEmailAlreadyVerified).
		WithReason(domain.ReasonEmailAlreadyVerified)
}
