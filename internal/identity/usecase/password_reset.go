package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// RequestPasswordReset e-mails a password-reset OTP valid for 2 minutes
// (FR-IAM-04). It answers the same way whether or not the e-mail belongs to
// an account, so it cannot be used to discover accounts.
func (uc *IdentityUseCase) RequestPasswordReset(ctx context.Context, rawEmail string) (Challenge, error) {
	now := uc.now().UTC()
	generic := Challenge{ExpiresAt: now.Add(domain.OTPTTL), ResendAvailableAt: now.Add(domain.OTPResendCooldown)}

	email, ok := domain.NormalizeEmail(rawEmail)
	if !ok {
		return Challenge{}, apperror.Validation("email", apperror.IssueInvalid, "email is invalid")
	}

	acc, err := uc.accounts.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return generic, nil
		}

		return Challenge{}, apperror.Internal("find account by email", err)
	}

	if acc.Status == domain.AccountStatusSuspended {
		return generic, nil
	}

	challenge, err := uc.sendOTP(ctx, domain.OTPPasswordReset, acc)
	if err != nil {
		if tooSoon, isTooSoon := errors.AsType[*resendTooSoonError](err); isTooSoon {
			return tooSoon.challenge, nil
		}

		slog.Error("identity: sending the password reset e-mail failed", "account_id", acc.ID, "error", err)

		return generic, nil
	}

	return challenge, nil
}

// VerifyPasswordResetOTP checks the reset OTP and returns a single-use
// reset token valid for 10 minutes.
func (uc *IdentityUseCase) VerifyPasswordResetOTP(ctx context.Context, rawEmail, code string) (string, time.Time, error) {
	email, ok := domain.NormalizeEmail(rawEmail)
	if !ok {
		return "", time.Time{}, otpError(domain.ErrOTPInvalid)
	}

	acc, err := uc.accounts.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return "", time.Time{}, otpError(domain.ErrOTPExpired)
		}

		return "", time.Time{}, apperror.Internal("find account by email", err)
	}

	if err := uc.checkOTP(ctx, domain.OTPPasswordReset, acc.ID, code); err != nil {
		return "", time.Time{}, err
	}

	token := auth.NewOpaqueToken()
	if err := uc.sessions.SaveResetToken(ctx, token, acc.ID, domain.ResetTokenTTL); err != nil {
		return "", time.Time{}, apperror.Internal("store reset token", err)
	}

	return token, uc.now().UTC().Add(domain.ResetTokenTTL), nil
}

// ResetPassword sets a new password with a reset token and signs every
// session out. Receiving the OTP proved control of the e-mail, so the
// address counts as verified.
func (uc *IdentityUseCase) ResetPassword(ctx context.Context, token, newPassword string) error {
	if !domain.ValidPassword(newPassword) {
		return apperror.Validation("newPassword", apperror.IssueTooShort, "password must have at least 8 characters")
	}

	accountID, err := uc.sessions.ConsumeResetToken(ctx, token)
	if err != nil {
		if errors.Is(err, domain.ErrResetTokenInvalid) {
			return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(domain.ReasonResetTokenInvalid)
		}

		return apperror.Internal("read reset token", err)
	}

	acc, err := uc.findAccount(ctx, accountID)
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return apperror.Internal("hash password", err)
	}

	passwordHash := string(hash)
	acc.PasswordHash = &passwordHash

	if acc.EmailVerified() {
		if err := uc.accounts.Update(ctx, acc); err != nil {
			return apperror.Internal("update password", err)
		}
	} else if err := uc.markVerified(ctx, acc); err != nil {
		return apperror.Internal("update password", err)
	}

	if err := uc.sessions.RevokeAllRefreshTokens(ctx, acc.ID); err != nil {
		return apperror.Internal("revoke sessions", err)
	}

	return nil
}
