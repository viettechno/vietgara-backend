package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"golang.org/x/crypto/bcrypt"
)

// AuthResult is returned by every login-style action (Register, Login,
// SocialLogin, Refresh) -- a consistent shape the handler layer can map
// straight into the AuthResponse proto message.
type AuthResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Account      *domain.Account
	// Garages is the same (garage_id, role) list baked into the access
	// token claim, echoed in AuthResponse for the frontend.
	Garages []GarageRole
}

// LoginRequest is the input for IdentityUseCase.Login. Exactly one of
// (Email, Password) or (Phone, OTPCode) must be set.
type LoginRequest struct {
	Email    *string
	Phone    *string
	Password *string // set for email login
	OTPCode  *string // set for phone login
}

// Login authenticates an account via email+password or phone+OTP
// (FR-IAM-02) and issues a fresh token pair whose "garages" claim is
// built from the Tenant module's garage_staff data.
func (uc *IdentityUseCase) Login(ctx context.Context, req LoginRequest) (*AuthResult, error) {
	var (
		acc *domain.Account
		err error
	)

	switch {
	case req.Email != nil && req.Password != nil:
		acc, err = uc.loginByPassword(ctx, *req.Email, *req.Password)
		if err != nil {
			return nil, err
		}

	case req.Phone != nil && req.OTPCode != nil:
		acc, err = uc.loginByOTP(ctx, *req.Phone, *req.OTPCode)
		if err != nil {
			return nil, err
		}

	default:
		return nil, apperror.New(apperror.CodeInvalidArgument, "must provide email+password or phone+otp_code")
	}

	if acc.Status == domain.AccountStatusSuspended {
		return nil, apperror.Wrap(apperror.CodeFailedPrecondition, "account suspended", domain.ErrAccountSuspended)
	}

	return uc.issueSession(ctx, acc)
}

// loginByPassword verifies the email/password pair. Unknown emails return
// the same Unauthenticated error as a wrong password, so the API cannot be
// used to enumerate registered accounts.
func (uc *IdentityUseCase) loginByPassword(ctx context.Context, email, password string) (*domain.Account, error) {
	acc, err := uc.accountRepo.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return nil, apperror.Wrap(apperror.CodeUnauthenticated, "invalid credentials", domain.ErrInvalidCredential)
		}

		return nil, apperror.Wrap(apperror.CodeInternal, "find account by email", err)
	}

	if acc.PasswordHash == nil || bcrypt.CompareHashAndPassword([]byte(*acc.PasswordHash), []byte(password)) != nil {
		return nil, apperror.Wrap(apperror.CodeUnauthenticated, "invalid credentials", domain.ErrInvalidCredential)
	}

	return acc, nil
}

// loginByOTP verifies the phone/OTP pair against the OTP verifier and
// looks up the matching account.
func (uc *IdentityUseCase) loginByOTP(ctx context.Context, phone, otpCode string) (*domain.Account, error) {
	ok, otpErr := uc.otpVerifier.Verify(ctx, phone, otpCode)
	if otpErr != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "verify otp", otpErr)
	}

	if !ok {
		return nil, apperror.Wrap(apperror.CodeUnauthenticated, "invalid otp", domain.ErrInvalidOTP)
	}

	acc, err := uc.accountRepo.FindByPhone(ctx, phone)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return nil, apperror.Wrap(apperror.CodeUnauthenticated, "account not found", domain.ErrAccountNotFound)
		}

		return nil, apperror.Wrap(apperror.CodeInternal, "find account by phone", err)
	}

	return acc, nil
}

// issueSession builds the "garages" claim from the Tenant module and
// issues a fresh access + refresh token pair. Shared by Login, SocialLogin,
// and Register flows.
func (uc *IdentityUseCase) issueSession(ctx context.Context, acc *domain.Account) (*AuthResult, error) {
	roles, err := uc.garageReader.ListGarageRolesForAccount(ctx, acc.ID)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "list garage roles", err)
	}

	claims := make([]auth.GarageClaim, 0, len(roles))
	for _, r := range roles {
		claims = append(claims, auth.GarageClaim{GarageID: r.GarageID, Role: r.Role})
	}

	accessToken, expiresAt, err := uc.issuer.IssueAccessToken(acc.ID, claims)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "issue access token", err)
	}

	refreshToken := auth.NewRefreshToken()
	if err := uc.tokenStore.Store(ctx, refreshToken, acc.ID, int(uc.refreshTTL.Seconds())); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "store refresh token", err)
	}

	return &AuthResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		Account:      acc,
		Garages:      roles,
	}, nil
}
