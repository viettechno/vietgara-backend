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
	"github.com/viettechno/vietgara-backend/pkg/email"
)

// RequestPasswordReset e-mails a password-reset OTP valid for 2 minutes
// (FR-IAM-04). It answers the same way whether or not the e-mail belongs to
// a user, so it cannot be used to discover users.
func (uc *IdentityUseCase) RequestPasswordReset(ctx context.Context, rawEmail string) (Challenge, error) {
	now := uc.now().UTC()
	generic := Challenge{ExpiresAt: now.Add(domain.OTPTTL), ResendAvailableAt: now.Add(domain.OTPResendCooldown)}

	addr, err := email.Normalize(rawEmail)
	if err != nil {
		return Challenge{}, apperror.Validation("email", apperror.IssueInvalid, "email is invalid")
	}

	user, err := uc.users.FindByEmail(ctx, addr)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return generic, nil
		}

		return Challenge{}, apperror.Internal("find user by email", err)
	}

	if user.Status == domain.UserStatusSuspended {
		return generic, nil
	}

	challenge, err := uc.sendOTP(ctx, domain.OTPPasswordReset, user)
	if err != nil {
		if tooSoon, isTooSoon := errors.AsType[*resendTooSoonError](err); isTooSoon {
			return tooSoon.challenge, nil
		}

		slog.Error("identity: sending the password reset e-mail failed", "user_id", user.ID, "error", err)

		return generic, nil
	}

	return challenge, nil
}

// VerifyPasswordResetOTP checks the reset OTP and returns a single-use
// reset token valid for 10 minutes.
func (uc *IdentityUseCase) VerifyPasswordResetOTP(ctx context.Context, rawEmail, code string) (string, time.Time, error) {
	addr, err := email.Normalize(rawEmail)
	if err != nil {
		return "", time.Time{}, otpError(domain.ErrOTPInvalid)
	}

	user, err := uc.users.FindByEmail(ctx, addr)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return "", time.Time{}, otpError(domain.ErrOTPExpired)
		}

		return "", time.Time{}, apperror.Internal("find user by email", err)
	}

	if err := uc.checkOTP(ctx, domain.OTPPasswordReset, user.ID, code); err != nil {
		return "", time.Time{}, err
	}

	token := auth.NewOpaqueToken()
	if err := uc.sessions.SaveResetToken(ctx, token, user.ID, domain.ResetTokenTTL); err != nil {
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

	userID, err := uc.sessions.ConsumeResetToken(ctx, token)
	if err != nil {
		if errors.Is(err, domain.ErrResetTokenInvalid) {
			return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(domain.ReasonResetTokenInvalid)
		}

		return apperror.Internal("read reset token", err)
	}

	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return apperror.Internal("hash password", err)
	}

	passwordHash := string(hash)
	user.PasswordHash = &passwordHash

	if user.EmailVerified() {
		if err := uc.users.Update(ctx, user); err != nil {
			return apperror.Internal("update password", err)
		}
	} else if err := uc.markVerified(ctx, user); err != nil {
		return apperror.Internal("update password", err)
	}

	if err := uc.sessions.RevokeAllRefreshTokens(ctx, user.ID); err != nil {
		return apperror.Internal("revoke sessions", err)
	}

	return nil
}
