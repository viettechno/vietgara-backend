package domain

import "errors"

// Sentinel errors for business-rule violations in the Identity module.
// These are wrapped into *apperror.AppError at the usecase boundary before
// being returned to the handler layer -- see internal/identity/usecase.
var (
	ErrInvalidCredential    = errors.New("invalid email or password")
	ErrAccountNotFound      = errors.New("account not found")
	ErrAccountSuspended     = errors.New("account is suspended")
	ErrEmailAlreadyExists   = errors.New("an account with this email already exists")
	ErrPhoneAlreadyExists   = errors.New("an account with this phone number already exists")
	ErrEmailAlreadyVerified = errors.New("the email address is already verified")
	ErrOTPInvalid           = errors.New("the code is incorrect")
	ErrOTPExpired           = errors.New("the code has expired, request a new one")
	ErrOTPTooManyAttempts   = errors.New("too many wrong codes, request a new one")
	ErrOTPResendTooSoon     = errors.New("wait before requesting a new code")
	ErrResetTokenInvalid    = errors.New("the password reset link is invalid or expired")
	ErrRefreshTokenInvalid  = errors.New("invalid or revoked refresh token")
)

// Client-facing reasons of the errors above.
//
//nolint:gosec // G101: error codes, not credentials
const (
	ReasonInvalidCredentials   = "INVALID_CREDENTIALS"
	ReasonAccountSuspended     = "ACCOUNT_SUSPENDED"
	ReasonEmailTaken           = "EMAIL_TAKEN"
	ReasonPhoneTaken           = "PHONE_TAKEN"
	ReasonEmailAlreadyVerified = "EMAIL_ALREADY_VERIFIED"
	ReasonOTPInvalid           = "OTP_INVALID"
	ReasonOTPExpired           = "OTP_EXPIRED"
	ReasonOTPTooManyAttempts   = "OTP_TOO_MANY_ATTEMPTS"
	ReasonOTPResendTooSoon     = "OTP_RESEND_TOO_SOON"
	ReasonResetTokenInvalid    = "RESET_TOKEN_INVALID"
	ReasonRefreshTokenInvalid  = "REFRESH_TOKEN_INVALID"
)
