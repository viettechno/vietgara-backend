package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// SendEmailVerificationOTP e-mails a new verification OTP (valid for 2
// minutes) to the signed-in account (FR-IAM-01).
func (uc *IdentityUseCase) SendEmailVerificationOTP(ctx context.Context, accountID string) (Challenge, error) {
	acc, err := uc.findAccount(ctx, accountID)
	if err != nil {
		return Challenge{}, err
	}

	if acc.EmailVerified() {
		return Challenge{}, alreadyVerified()
	}

	challenge, err := uc.sendOTP(ctx, domain.OTPEmailVerification, acc)
	if err != nil {
		if errors.Is(err, domain.ErrOTPResendTooSoon) {
			return Challenge{}, otpError(err)
		}

		return Challenge{}, apperror.Internal("send verification otp", err)
	}

	return challenge, nil
}

// VerifyEmail confirms the signed-in account's e-mail with the OTP,
// publishes account.email_verified (default roles and trial follow), and
// rotates the session so the new access token is marked verified.
func (uc *IdentityUseCase) VerifyEmail(ctx context.Context, accountID, code, refreshToken string) (*Session, error) {
	acc, err := uc.findAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	if acc.EmailVerified() {
		return nil, alreadyVerified()
	}

	if err := uc.checkOTP(ctx, domain.OTPEmailVerification, acc.ID, code); err != nil {
		return nil, err
	}

	if err := uc.markVerified(ctx, acc); err != nil {
		return nil, apperror.Internal("mark email verified", err)
	}

	if owner, err := uc.sessions.RefreshTokenAccount(ctx, refreshToken); err == nil && owner == acc.ID {
		_ = uc.sessions.RevokeRefreshToken(ctx, refreshToken)
	}

	return uc.issueSession(ctx, acc)
}

func alreadyVerified() error {
	return apperror.Wrap(apperror.CodeFailedPrecondition, domain.ErrEmailAlreadyVerified.Error(), domain.ErrEmailAlreadyVerified).
		WithReason(domain.ReasonEmailAlreadyVerified)
}
