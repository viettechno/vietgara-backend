package domain

import "errors"

// Sentinel errors for business-rule violations in the Identity module.
// These are wrapped into *apperror.AppError at the usecase boundary before
// being returned to the handler layer -- see internal/identity/usecase.
var (
	ErrInvalidCredential  = errors.New("invalid email/phone or password")
	ErrAccountNotFound    = errors.New("account not found")
	ErrAccountSuspended   = errors.New("account is suspended")
	ErrEmailAlreadyExists = errors.New("an account with this email already exists")
	ErrPhoneAlreadyExists = errors.New("an account with this phone number already exists")
	ErrInvalidOTP         = errors.New("invalid or expired OTP")
)
