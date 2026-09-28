// Package usecase contains the application use cases of the Identity
// module: they validate input, enforce business rules, and orchestrate the
// domain and repository layers.
// See: Coding_Standards.docx Section 2.3, LLD Section 6.
package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// IdentityUseCase implements every action of the Identity module:
// registration, sign-in (e-mail/password and Google), sessions, e-mail
// verification, password reset and the account profile.
type IdentityUseCase struct {
	accounts AccountRepository
	sessions SessionStore
	otps     OTPStore
	mailer   Mailer
	events   EventPublisher
	tx       Transactor
	issuer   *auth.TokenIssuer
	now      func() time.Time
}

// Option customizes an IdentityUseCase.
type Option func(*IdentityUseCase)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option {
	return func(uc *IdentityUseCase) { uc.now = now }
}

// NewIdentityUseCase wires the identity use case to its dependencies.
func NewIdentityUseCase(
	accounts AccountRepository,
	sessions SessionStore,
	otps OTPStore,
	mailer Mailer,
	events EventPublisher,
	tx Transactor,
	issuer *auth.TokenIssuer,
	opts ...Option,
) *IdentityUseCase {
	uc := &IdentityUseCase{
		accounts: accounts, sessions: sessions, otps: otps, mailer: mailer,
		events: events, tx: tx, issuer: issuer, now: time.Now,
	}

	for _, opt := range opts {
		opt(uc)
	}

	return uc
}

// Session is a signed-in session.
type Session struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Account      *domain.Account
}

// Challenge tells the client how long an e-mailed OTP is valid.
type Challenge struct {
	ExpiresAt         time.Time
	ResendAvailableAt time.Time
}

// issueSession issues an access token carrying the account's verification
// and admin flags plus a stored refresh token.
func (uc *IdentityUseCase) issueSession(ctx context.Context, acc *domain.Account) (*Session, error) {
	if acc.Status == domain.AccountStatusSuspended {
		return nil, apperror.Wrap(apperror.CodePermissionDenied, domain.ErrAccountSuspended.Error(), domain.ErrAccountSuspended).
			WithReason(domain.ReasonAccountSuspended)
	}

	accessToken, expiresAt, err := uc.issuer.IssueAccessToken(auth.Identity{
		AccountID: acc.ID, EmailVerified: acc.EmailVerified(), PlatformAdmin: acc.PlatformAdmin,
	})
	if err != nil {
		return nil, apperror.Internal("issue access token", err)
	}

	refreshToken := auth.NewOpaqueToken()
	if err := uc.sessions.SaveRefreshToken(ctx, refreshToken, acc.ID, uc.issuer.RefreshTTL()); err != nil {
		return nil, apperror.Internal("store refresh token", err)
	}

	return &Session{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt, Account: acc}, nil
}

// markVerified records the e-mail verification and publishes
// account.email_verified in the same transaction; a no-op when already
// verified.
func (uc *IdentityUseCase) markVerified(ctx context.Context, acc *domain.Account) error {
	if !acc.MarkEmailVerified(uc.now().UTC()) {
		return nil
	}

	return uc.tx.WithTransaction(ctx, platformdb.AccountScope(acc.ID), func(ctx context.Context) error {
		if err := uc.accounts.Update(ctx, acc); err != nil {
			return err
		}

		return uc.events.Publish(ctx, EventAccountEmailVerified, "", acc.ID, map[string]any{
			"account_id":  acc.ID,
			"email":       acc.Email,
			"full_name":   acc.FullName,
			"locale":      acc.Locale.OrDefault(),
			"verified_at": acc.EmailVerifiedAt.Format(time.RFC3339),
		})
	})
}

// findAccount loads an account, mapping "not found" to notFound.
func (uc *IdentityUseCase) findAccount(ctx context.Context, id string) (*domain.Account, error) {
	acc, err := uc.accounts.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return nil, apperror.Wrap(apperror.CodeUnauthenticated, "account not found", err)
		}

		return nil, apperror.Internal("find account", err)
	}

	return acc, nil
}

// GrantPlatformAdmins makes the accounts with these e-mails platform
// admins (PLATFORM_ADMIN_EMAILS at startup).
func (uc *IdentityUseCase) GrantPlatformAdmins(ctx context.Context, emails []string) error {
	normalized := make([]string, 0, len(emails))

	for _, raw := range emails {
		if email, ok := domain.NormalizeEmail(raw); ok {
			normalized = append(normalized, email)
		}
	}

	if len(normalized) == 0 {
		return nil
	}

	granted, err := uc.accounts.GrantPlatformAdmins(ctx, normalized)
	if err != nil {
		return apperror.Internal("grant platform admins", err)
	}

	slog.Info("platform admins granted", "configured", len(normalized), "granted", granted)

	return nil
}

func invalidCredentials() error {
	return apperror.Wrap(apperror.CodeUnauthenticated, domain.ErrInvalidCredential.Error(), domain.ErrInvalidCredential).
		WithReason(domain.ReasonInvalidCredentials)
}
