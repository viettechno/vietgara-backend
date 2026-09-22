package usecase

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// RegisterRequest is the input for IdentityUseCase.Register.
// See: FRD FR-IAM-01.
type RegisterRequest struct {
	Email    *string
	Phone    *string
	Password string // required when registering via email; empty for phone/OTP-only accounts
	FullName string
}

// Register validates the request, rejects duplicate identifiers, persists
// the new account, and issues a full session (access + refresh token) so
// the frontend can go straight from registration into the authenticated
// app.
func (uc *IdentityUseCase) Register(ctx context.Context, req RegisterRequest) (*AuthResult, error) {
	if req.Email == nil && req.Phone == nil {
		return nil, apperror.New(apperror.CodeInvalidArgument, "email or phone is required")
	}

	if req.FullName == "" {
		return nil, apperror.New(apperror.CodeInvalidArgument, "full_name is required")
	}

	if req.Email != nil {
		if err := uc.ensureEmailAvailable(ctx, *req.Email); err != nil {
			return nil, err
		}
	}

	if req.Phone != nil {
		if err := uc.ensurePhoneAvailable(ctx, *req.Phone); err != nil {
			return nil, err
		}
	}

	var passwordHash *string

	if req.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, apperror.Wrap(apperror.CodeInternal, "hash password", err)
		}

		s := string(hash)
		passwordHash = &s
	}

	acc := &domain.Account{
		ID:           uuid.NewString(),
		Email:        req.Email,
		Phone:        req.Phone,
		AuthProvider: domain.AuthProviderEmail,
		FullName:     req.FullName,
		PasswordHash: passwordHash,
		Status:       domain.AccountStatusActive,
	}

	if err := uc.accountRepo.Create(ctx, acc); err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "create account", err)
	}

	return uc.issueSession(ctx, acc)
}

// ensureEmailAvailable rejects an email that is already registered.
func (uc *IdentityUseCase) ensureEmailAvailable(ctx context.Context, email string) error {
	existing, err := uc.accountRepo.FindByEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrAccountNotFound) {
		return apperror.Wrap(apperror.CodeInternal, "check existing email", err)
	}

	if existing != nil {
		return apperror.Wrap(apperror.CodeAlreadyExists, "email already registered", domain.ErrEmailAlreadyExists)
	}

	return nil
}

// ensurePhoneAvailable rejects a phone that is already registered.
func (uc *IdentityUseCase) ensurePhoneAvailable(ctx context.Context, phone string) error {
	existing, err := uc.accountRepo.FindByPhone(ctx, phone)
	if err != nil && !errors.Is(err, domain.ErrAccountNotFound) {
		return apperror.Wrap(apperror.CodeInternal, "check existing phone", err)
	}

	if existing != nil {
		return apperror.Wrap(apperror.CodeAlreadyExists, "phone already registered", domain.ErrPhoneAlreadyExists)
	}

	return nil
}
