package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// GoogleProfile is the caller's identity as returned by Google's userinfo
// endpoint after the OAuth code exchange.
type GoogleProfile struct {
	Subject       string
	Email         string
	EmailVerified bool
	FullName      string
	Locale        domain.Locale
}

// GoogleLogin signs an account in with Google (FR-IAM-01/02). The account
// is found by its Google subject, else by its (Google-verified) e-mail --
// which links Google to an existing e-mail account -- else created. Google
// verified the e-mail, so the account is marked verified.
func (uc *IdentityUseCase) GoogleLogin(ctx context.Context, profile GoogleProfile) (*Session, error) {
	email, ok := domain.NormalizeEmail(profile.Email)
	if profile.Subject == "" || !ok || !profile.EmailVerified {
		return nil, apperror.New(apperror.CodeUnauthenticated, "Google did not provide a verified e-mail address").
			WithReason(domain.ReasonInvalidCredentials)
	}

	acc, err := uc.findGoogleAccount(ctx, profile.Subject, email)
	if err != nil {
		return nil, err
	}

	if acc == nil {
		acc, err = uc.createGoogleAccount(ctx, profile, email)
		if err != nil {
			return nil, err
		}
	} else if acc.GoogleSubject == nil {
		subject := profile.Subject
		acc.GoogleSubject = &subject

		if err := uc.accounts.Update(ctx, acc); err != nil {
			return nil, apperror.Internal("link google account", err)
		}
	}

	if err := uc.markVerified(ctx, acc); err != nil {
		return nil, apperror.Internal("verify google account", err)
	}

	return uc.issueSession(ctx, acc)
}

func (uc *IdentityUseCase) findGoogleAccount(ctx context.Context, subject, email string) (*domain.Account, error) {
	acc, err := uc.accounts.FindByGoogleSubject(ctx, subject)
	if err == nil {
		return acc, nil
	}

	if !errors.Is(err, domain.ErrAccountNotFound) {
		return nil, apperror.Internal("find account by google subject", err)
	}

	acc, err = uc.accounts.FindByEmail(ctx, email)
	if err == nil {
		return acc, nil
	}

	if !errors.Is(err, domain.ErrAccountNotFound) {
		return nil, apperror.Internal("find account by email", err)
	}

	return nil, nil //nolint:nilnil // no account yet
}

func (uc *IdentityUseCase) createGoogleAccount(ctx context.Context, profile GoogleProfile, email string) (*domain.Account, error) {
	fullName, ok := domain.NormalizeFullName(profile.FullName)
	if !ok {
		fullName = email
	}

	subject := profile.Subject
	acc := &domain.Account{
		Email:         email,
		GoogleSubject: &subject,
		AuthProvider:  domain.AuthProviderGoogle,
		FullName:      fullName,
		Status:        domain.AccountStatusActive,
		Locale:        profile.Locale.OrDefault(),
	}

	err := uc.tx.WithTransaction(ctx, platformdb.Scope{}, func(ctx context.Context) error {
		return uc.accounts.Create(ctx, acc)
	})
	if err != nil {
		return nil, mapCreateError(err)
	}

	return acc, nil
}
