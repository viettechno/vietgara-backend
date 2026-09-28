package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

// EventAccountEmailVerified is published once an account's e-mail is
// verified (by OTP, password reset, or Google); the Tenant module creates
// the owner's default roles and the License module starts the trial.
const EventAccountEmailVerified = "account.email_verified"

// AccountRepository is implemented by
// internal/identity/repository.PostgresAccountRepository. Finders return
// domain.ErrAccountNotFound when nothing matches.
type AccountRepository interface {
	FindByID(ctx context.Context, id string) (*domain.Account, error)
	// FindByEmail matches case-insensitively.
	FindByEmail(ctx context.Context, email string) (*domain.Account, error)
	// FindByPhone matches the normalized phone.
	FindByPhone(ctx context.Context, phone string) (*domain.Account, error)
	FindByGoogleSubject(ctx context.Context, subject string) (*domain.Account, error)
	// Create inserts acc, filling ID and timestamps; a duplicate e-mail or
	// phone yields domain.ErrEmailAlreadyExists / ErrPhoneAlreadyExists.
	Create(ctx context.Context, acc *domain.Account) error
	// Update persists the mutable fields of acc (name, phone, locale,
	// password, verification, Google link).
	Update(ctx context.Context, acc *domain.Account) error
	// GrantPlatformAdmins makes the accounts with these e-mails platform
	// admins and returns how many were granted.
	GrantPlatformAdmins(ctx context.Context, emails []string) (int, error)
}

// SessionStore keeps refresh and password-reset tokens (Redis in
// production, Security Architecture Section 2.2) so they can be revoked.
type SessionStore interface {
	SaveRefreshToken(ctx context.Context, token, accountID string, ttl time.Duration) error
	// RefreshTokenAccount returns domain.ErrRefreshTokenInvalid for unknown
	// or revoked tokens.
	RefreshTokenAccount(ctx context.Context, token string) (string, error)
	RevokeRefreshToken(ctx context.Context, token string) error
	RevokeAllRefreshTokens(ctx context.Context, accountID string) error
	SaveResetToken(ctx context.Context, token, accountID string, ttl time.Duration) error
	// ConsumeResetToken returns the account and deletes the token, or
	// domain.ErrResetTokenInvalid.
	ConsumeResetToken(ctx context.Context, token string) (string, error)
}

// OTPStore keeps issued OTPs until they expire.
type OTPStore interface {
	// Get returns (nil, nil) when no OTP is stored under key.
	Get(ctx context.Context, key string) (*domain.OTP, error)
	// Put stores otp until its expiry.
	Put(ctx context.Context, key string, otp *domain.OTP) error
	// IncrementAttempts counts a wrong code and returns the new total.
	IncrementAttempts(ctx context.Context, key string) (int, error)
	Delete(ctx context.Context, key string) error
}

// Mailer delivers e-mails (internal/platform/mail).
type Mailer interface {
	Send(ctx context.Context, msg mail.Message) error
}

// EventPublisher writes domain events to the outbox in the caller's
// transaction (internal/platform/outbox).
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, accountID string, payload any) error
}

// Transactor runs fn in a scoped transaction (see db.TxManager).
type Transactor interface {
	WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error
}
