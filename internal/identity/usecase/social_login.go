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

// GoogleLogin signs a user in with Google (FR-IAM-01/02). The user
// is found by its Google subject, else by its (Google-verified) e-mail --
// which links Google to an existing e-mail user -- else created. Google
// verified the e-mail, so the user is marked verified.
func (uc *IdentityUseCase) GoogleLogin(ctx context.Context, profile GoogleProfile) (*Session, error) {
	email, ok := domain.NormalizeEmail(profile.Email)
	if profile.Subject == "" || !ok || !profile.EmailVerified {
		return nil, apperror.New(apperror.CodeUnauthenticated, "Google did not provide a verified e-mail address").
			WithReason(domain.ReasonInvalidCredentials)
	}

	user, err := uc.findGoogleUser(ctx, profile.Subject, email)
	if err != nil {
		return nil, err
	}

	if user == nil {
		user, err = uc.createGoogleUser(ctx, profile, email)
		if err != nil {
			return nil, err
		}
	} else if user.GoogleSubject == nil {
		subject := profile.Subject
		user.GoogleSubject = &subject

		if err := uc.users.Update(ctx, user); err != nil {
			return nil, apperror.Internal("link google user", err)
		}
	}

	if err := uc.markVerified(ctx, user); err != nil {
		return nil, apperror.Internal("verify google user", err)
	}

	return uc.issueSession(ctx, user)
}

func (uc *IdentityUseCase) findGoogleUser(ctx context.Context, subject, email string) (*domain.User, error) {
	user, err := uc.users.FindByGoogleSubject(ctx, subject)
	if err == nil {
		return user, nil
	}

	if !errors.Is(err, domain.ErrUserNotFound) {
		return nil, apperror.Internal("find user by google subject", err)
	}

	user, err = uc.users.FindByEmail(ctx, email)
	if err == nil {
		return user, nil
	}

	if !errors.Is(err, domain.ErrUserNotFound) {
		return nil, apperror.Internal("find user by email", err)
	}

	return nil, nil //nolint:nilnil // no user yet
}

func (uc *IdentityUseCase) createGoogleUser(ctx context.Context, profile GoogleProfile, email string) (*domain.User, error) {
	fullName, ok := domain.NormalizeFullName(profile.FullName)
	if !ok {
		fullName = email
	}

	subject := profile.Subject
	user := &domain.User{
		Email:         email,
		GoogleSubject: &subject,
		AuthProvider:  domain.AuthProviderGoogle,
		FullName:      fullName,
		Status:        domain.UserStatusActive,
		Locale:        profile.Locale.OrDefault(),
	}

	err := uc.tx.WithTransaction(ctx, platformdb.Scope{}, func(ctx context.Context) error {
		return uc.users.Create(ctx, user)
	})
	if err != nil {
		return nil, mapCreateError(err)
	}

	return user, nil
}
