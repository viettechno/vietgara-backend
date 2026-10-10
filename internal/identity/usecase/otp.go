package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

var otpTemplates = map[domain.OTPPurpose]string{
	domain.OTPEmailVerification: mail.TemplateEmailVerification,
	domain.OTPPasswordReset:     mail.TemplatePasswordReset,
}

// resendTooSoonError carries the challenge still running.
type resendTooSoonError struct{ challenge Challenge }

func (e *resendTooSoonError) Error() string { return domain.ErrOTPResendTooSoon.Error() }
func (e *resendTooSoonError) Unwrap() error { return domain.ErrOTPResendTooSoon }

// sendOTP issues a new OTP of purpose for user and e-mails it in the
// user's language. Within the resend cooldown it returns
// *resendTooSoonError with the running challenge.
func (uc *IdentityUseCase) sendOTP(ctx context.Context, purpose domain.OTPPurpose, user *domain.User) (Challenge, error) {
	key := domain.OTPKey(purpose, user.ID)
	now := uc.now().UTC()

	existing, err := uc.otps.Get(ctx, key)
	if err != nil {
		return Challenge{}, err
	}

	if existing != nil && now.Before(existing.ResendAt) && !existing.Expired(now) {
		return Challenge{}, &resendTooSoonError{challenge: Challenge{ExpiresAt: existing.ExpiresAt, ResendAvailableAt: existing.ResendAt}}
	}

	code := domain.NewOTPCode()
	otp := domain.NewOTP(key, code, now)

	msg, err := mail.Render(otpTemplates[purpose], string(user.Locale.OrDefault()), user.Email, user.FullName, map[string]any{
		"Name": user.FullName, "Code": code, "Minutes": int(domain.OTPTTL.Minutes()),
	})
	if err != nil {
		return Challenge{}, err
	}

	if err := uc.otps.Put(ctx, key, otp); err != nil {
		return Challenge{}, err
	}

	if err := uc.mailer.Send(ctx, msg); err != nil {
		_ = uc.otps.Delete(ctx, key)

		return Challenge{}, err
	}

	return Challenge{ExpiresAt: otp.ExpiresAt, ResendAvailableAt: otp.ResendAt}, nil
}

// checkOTP verifies code against the OTP of purpose for userID and
// consumes it on success. Wrong codes count towards OTPMaxAttempts.
func (uc *IdentityUseCase) checkOTP(ctx context.Context, purpose domain.OTPPurpose, userID, code string) error {
	key := domain.OTPKey(purpose, userID)

	otp, err := uc.otps.Get(ctx, key)
	if err != nil {
		return apperror.Internal("read otp", err)
	}

	switch {
	case otp == nil:
		return otpError(domain.ErrOTPExpired)
	case otp.Expired(uc.now().UTC()):
		_ = uc.otps.Delete(ctx, key)

		return otpError(domain.ErrOTPExpired)
	case otp.Attempts >= domain.OTPMaxAttempts:
		return otpError(domain.ErrOTPTooManyAttempts)
	}

	if !otp.Matches(key, code) {
		attempts, err := uc.otps.IncrementAttempts(ctx, key)
		if err != nil {
			return apperror.Internal("count otp attempt", err)
		}

		if attempts >= domain.OTPMaxAttempts {
			return otpError(domain.ErrOTPTooManyAttempts)
		}

		return otpError(domain.ErrOTPInvalid)
	}

	if err := uc.otps.Delete(ctx, key); err != nil {
		return apperror.Internal("consume otp", err)
	}

	return nil
}

func otpError(err error) error {
	switch {
	case errors.Is(err, domain.ErrOTPExpired):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(domain.ReasonOTPExpired).WithField("otpCode", "expired")
	case errors.Is(err, domain.ErrOTPTooManyAttempts):
		return apperror.Wrap(apperror.CodeResourceExhausted, err.Error(), err).WithReason(domain.ReasonOTPTooManyAttempts)
	case errors.Is(err, domain.ErrOTPResendTooSoon):
		return apperror.Wrap(apperror.CodeResourceExhausted, err.Error(), err).WithReason(domain.ReasonOTPResendTooSoon)
	default:
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(domain.ReasonOTPInvalid).WithField("otpCode", apperror.IssueInvalid)
	}
}
