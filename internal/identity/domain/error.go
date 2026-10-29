package domain

import "errors"

// Sentinel errors for business-rule violations in the Identity module.
// These are wrapped into *apperror.AppError at the usecase boundary before
// being returned to the handler layer -- see internal/identity/usecase.
var (
	ErrInvalidCredential    = errors.New("invalid email or password")
	ErrUserNotFound         = errors.New("user not found")
	ErrUserSuspended        = errors.New("user is suspended")
	ErrEmailAlreadyExists   = errors.New("a user with this email already exists")
	ErrPhoneAlreadyExists   = errors.New("a user with this phone number already exists")
	ErrEmailAlreadyVerified = errors.New("the email address is already verified")
	ErrOTPInvalid           = errors.New("the code is incorrect")
	ErrOTPExpired           = errors.New("the code has expired, request a new one")
	ErrOTPTooManyAttempts   = errors.New("too many wrong codes, request a new one")
	ErrOTPResendTooSoon     = errors.New("wait before requesting a new code")
	ErrResetTokenInvalid    = errors.New("the password reset link is invalid or expired")
	ErrRefreshTokenInvalid  = errors.New("invalid or revoked refresh token")

	ErrMFAChallengeInvalid     = errors.New("the sign-in has expired, sign in again")
	ErrMFACodeInvalid          = errors.New("the code is incorrect")
	ErrMFALocked               = errors.New("too many wrong codes, try again later")
	ErrMFAAlreadyEnabled       = errors.New("two-factor authentication is already on")
	ErrMFANotEnabled           = errors.New("two-factor authentication is not on")
	ErrMFAEnrollmentNotStarted = errors.New("start the enrolment first")
	ErrMFANotAvailable         = errors.New("two-factor authentication needs an e-mail and password sign-in")
	ErrMFANotConfigured        = errors.New("two-factor authentication is not configured on this server")
	ErrMFANotFound             = errors.New("no two-factor enrolment")

	ErrPasswordIncorrect   = errors.New("the password is incorrect")
	ErrNoPassword          = errors.New("this user signs in with Google only: set a password first")
	ErrGoogleAlreadyLinked = errors.New("this Google account is already linked to a user")
	ErrGoogleLinkExists    = errors.New("a Google account is already linked: unlink it first")
	ErrGoogleNotLinked     = errors.New("no Google account is linked")
	ErrLinkProofInvalid    = errors.New("the Google link has expired, start it again")
	ErrGoogleSigninBlocked = errors.New("google sign-in is off for this user: link a Google account in the security settings")
)

// Client-facing reasons of the errors above.
//
//nolint:gosec // G101: error codes, not credentials
const (
	ReasonInvalidCredentials   = "INVALID_CREDENTIALS"
	ReasonUserSuspended        = "USER_SUSPENDED"
	ReasonEmailTaken           = "EMAIL_TAKEN"
	ReasonPhoneTaken           = "PHONE_TAKEN"
	ReasonEmailAlreadyVerified = "EMAIL_ALREADY_VERIFIED"
	ReasonOTPInvalid           = "OTP_INVALID"
	ReasonOTPExpired           = "OTP_EXPIRED"
	ReasonOTPTooManyAttempts   = "OTP_TOO_MANY_ATTEMPTS"
	ReasonOTPResendTooSoon     = "OTP_RESEND_TOO_SOON"
	ReasonResetTokenInvalid    = "RESET_TOKEN_INVALID"
	ReasonRefreshTokenInvalid  = "REFRESH_TOKEN_INVALID"

	ReasonMFAChallengeInvalid     = "MFA_CHALLENGE_INVALID"
	ReasonMFACodeInvalid          = "MFA_CODE_INVALID"
	ReasonMFALocked               = "MFA_LOCKED"
	ReasonMFAAlreadyEnabled       = "MFA_ALREADY_ENABLED"
	ReasonMFANotEnabled           = "MFA_NOT_ENABLED"
	ReasonMFAEnrollmentNotStarted = "MFA_ENROLLMENT_NOT_STARTED"
	ReasonMFANotAvailable         = "MFA_NOT_AVAILABLE"
	ReasonMFANotConfigured        = "MFA_NOT_CONFIGURED"

	ReasonPasswordIncorrect   = "PASSWORD_INCORRECT"
	ReasonNoPassword          = "NO_PASSWORD"
	ReasonGoogleAlreadyLinked = "GOOGLE_ALREADY_LINKED"
	ReasonGoogleLinkExists    = "GOOGLE_LINK_EXISTS"
	ReasonGoogleNotLinked     = "GOOGLE_NOT_LINKED"
	ReasonLinkProofInvalid    = "LINK_PROOF_INVALID"
	ReasonGoogleSigninBlocked = "GOOGLE_SIGNIN_BLOCKED"
)
