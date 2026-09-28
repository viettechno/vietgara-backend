package usecase

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"golang.org/x/crypto/bcrypt"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// RegisterRequest is the input for IdentityUseCase.Register (FR-IAM-01).
type RegisterRequest struct {
	Email    string
	Password string
	FullName string
	Phone    *string
	Locale   domain.Locale
}

// Register creates an unverified owner account, e-mails the verification
// OTP once the account is stored, and signs the new owner in: until the
// e-mail is verified only the profile and verification APIs are usable.
func (uc *IdentityUseCase) Register(ctx context.Context, req RegisterRequest) (*Session, error) {
	acc, password, err := newAccount(req)
	if err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperror.Internal("hash password", err)
	}

	passwordHash := string(hash)
	acc.PasswordHash = &passwordHash

	err = uc.tx.WithTransaction(ctx, platformdb.Scope{}, func(ctx context.Context) error {
		if err := uc.ensureAvailable(ctx, acc.Email, acc.Phone); err != nil {
			return err
		}

		if err := uc.accounts.Create(ctx, acc); err != nil {
			return mapCreateError(err)
		}

		platformdb.AfterCommit(ctx, func() {
			if _, err := uc.sendOTP(context.WithoutCancel(ctx), domain.OTPEmailVerification, acc); err != nil {
				slog.Warn("identity: sending the verification e-mail after sign-up failed", "account_id", acc.ID, "error", err)
			}
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	return uc.issueSession(ctx, acc)
}

func newAccount(req RegisterRequest) (*domain.Account, string, error) {
	email, ok := domain.NormalizeEmail(req.Email)
	if !ok {
		return nil, "", apperror.Validation("email", apperror.IssueInvalid, "email is invalid")
	}

	fullName, ok := domain.NormalizeFullName(req.FullName)
	if !ok {
		return nil, "", apperror.Validation("fullName", apperror.IssueRequired, "full name is required")
	}

	if !domain.ValidPassword(req.Password) {
		return nil, "", apperror.Validation("password", apperror.IssueTooShort, "password must have at least 8 characters")
	}

	phone, err := normalizeOptionalPhone(req.Phone)
	if err != nil {
		return nil, "", err
	}

	return &domain.Account{
		Email:        email,
		Phone:        phone,
		AuthProvider: domain.AuthProviderEmail,
		FullName:     fullName,
		Status:       domain.AccountStatusActive,
		Locale:       req.Locale.OrDefault(),
	}, req.Password, nil
}

// normalizeOptionalPhone validates an optional phone; "" clears it.
func normalizeOptionalPhone(raw *string) (*string, error) {
	if raw == nil || *raw == "" {
		return nil, nil //nolint:nilnil // no phone is a valid answer
	}

	phone, ok := domain.NormalizePhone(*raw)
	if !ok {
		return nil, apperror.Validation("phone", apperror.IssueInvalid, "phone number is invalid")
	}

	return &phone, nil
}

// ensureAvailable rejects an e-mail or phone that is already registered
// (to another account than exceptID).
func (uc *IdentityUseCase) ensureAvailable(ctx context.Context, email string, phone *string, exceptID ...string) error {
	if email != "" {
		existing, err := uc.accounts.FindByEmail(ctx, email)
		if err != nil && !errors.Is(err, domain.ErrAccountNotFound) {
			return apperror.Internal("check existing email", err)
		}

		if existing != nil && !isOneOf(existing.ID, exceptID) {
			return emailTaken()
		}
	}

	if phone != nil {
		existing, err := uc.accounts.FindByPhone(ctx, *phone)
		if err != nil && !errors.Is(err, domain.ErrAccountNotFound) {
			return apperror.Internal("check existing phone", err)
		}

		if existing != nil && !isOneOf(existing.ID, exceptID) {
			return phoneTaken()
		}
	}

	return nil
}

func isOneOf(id string, ids []string) bool {
	return slices.Contains(ids, id)
}

func emailTaken() error {
	return apperror.Wrap(apperror.CodeAlreadyExists, domain.ErrEmailAlreadyExists.Error(), domain.ErrEmailAlreadyExists).
		WithReason(domain.ReasonEmailTaken).WithField("email", apperror.IssueTaken)
}

func phoneTaken() error {
	return apperror.Wrap(apperror.CodeAlreadyExists, domain.ErrPhoneAlreadyExists.Error(), domain.ErrPhoneAlreadyExists).
		WithReason(domain.ReasonPhoneTaken).WithField("phone", apperror.IssueTaken)
}

// mapCreateError turns a unique-constraint race into the matching error.
func mapCreateError(err error) error {
	switch {
	case errors.Is(err, domain.ErrEmailAlreadyExists):
		return emailTaken()
	case errors.Is(err, domain.ErrPhoneAlreadyExists):
		return phoneTaken()
	default:
		return apperror.Internal("create account", err)
	}
}
