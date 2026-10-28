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
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/email"
)

// IdentityUseCase implements every action of the Identity module:
// registration, sign-in (e-mail/password and Google), sessions, e-mail
// verification, password reset and the user profile.
type IdentityUseCase struct {
	users    UserRepository
	sessions SessionStore
	otps     OTPStore
	mailer   Mailer
	events   EventPublisher
	tx       Transactor
	issuer   *auth.TokenIssuer
	now      func() time.Time
	// mfa and box are set by WithMFA; without them the MFA calls answer
	// "not configured" and the sign-in never asks for a second factor.
	mfa MFARepository
	box SecretBox
	// audit is optional (WithAudit).
	audit AuditLogger
	// platformAdminEmails grants platform admin to users whose e-mail is
	// one of these (PLATFORM_ADMIN_EMAILS) once it is verified, never
	// before: anyone can register an address they do not own (SR-25).
	platformAdminEmails map[string]struct{}
	// platformAccess, when set, audits the sign-ins of platform users.
	platformAccess PlatformAccessChecker
	// otpSecret keys the hash of the stored OTPs (WithOTPSecret).
	otpSecret []byte
}

// Option customizes an IdentityUseCase.
type Option func(*IdentityUseCase)

// WithClock replaces time.Now (tests).
func WithClock(now func() time.Time) Option {
	return func(uc *IdentityUseCase) { uc.now = now }
}

// WithOTPSecret keys the hash of the OTPs kept in the OTP store, so that a
// copy of the store cannot be used to find the 6-digit codes (SR-21).
func WithOTPSecret(secret []byte) Option {
	return func(uc *IdentityUseCase) { uc.otpSecret = secret }
}

// WithPlatformAdminEmails lets users with one of these e-mails become
// platform admins when their e-mail is verified.
func WithPlatformAdminEmails(emails []string) Option {
	return func(uc *IdentityUseCase) {
		uc.platformAdminEmails = make(map[string]struct{}, len(emails))
		for _, raw := range emails {
			if addr, err := email.Normalize(raw); err == nil {
				uc.platformAdminEmails[addr] = struct{}{}
			}
		}
	}
}

// WithMFA enables the TOTP second factor (FR-IAM-09): its enrolment is
// stored in repo and the secrets are encrypted with box.
func WithMFA(repo MFARepository, box SecretBox) Option {
	return func(uc *IdentityUseCase) { uc.mfa, uc.box = repo, box }
}

// PlatformAccessChecker tells whether a user has any platform access (a
// Platform Admin, or a member of a platform staff group).
type PlatformAccessChecker interface {
	HasPlatformAccess(ctx context.Context, userID string) (bool, error)
}

// WithPlatformSignInAudit writes every sign-in of a user with platform
// access to the audit log (Release 8.1, NFR-SE-07).
func WithPlatformSignInAudit(checker PlatformAccessChecker) Option {
	return func(uc *IdentityUseCase) { uc.platformAccess = checker }
}

// WithAudit records MFA changes in the audit log.
func WithAudit(logger AuditLogger) Option {
	return func(uc *IdentityUseCase) { uc.audit = logger }
}

// NewIdentityUseCase wires the identity use case to its dependencies.
func NewIdentityUseCase(
	users UserRepository,
	sessions SessionStore,
	otps OTPStore,
	mailer Mailer,
	events EventPublisher,
	tx Transactor,
	issuer *auth.TokenIssuer,
	opts ...Option,
) *IdentityUseCase {
	uc := &IdentityUseCase{
		users: users, sessions: sessions, otps: otps, mailer: mailer,
		events: events, tx: tx, issuer: issuer, now: time.Now,
	}

	for _, opt := range opts {
		opt(uc)
	}

	return uc
}

// Session is a signed-in session, or, when MFARequired is set, a sign-in
// waiting for the second factor: it then carries a ChallengeToken for
// VerifyMFAChallenge instead of tokens.
type Session struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	User         *domain.User

	MFARequired        bool
	ChallengeToken     string
	ChallengeExpiresAt time.Time
}

// Challenge tells the client how long an e-mailed OTP is valid.
type Challenge struct {
	ExpiresAt         time.Time
	ResendAvailableAt time.Time
}

// issueSession issues an access token carrying the user's verification
// and admin flags plus a stored refresh token.
func (uc *IdentityUseCase) issueSession(ctx context.Context, user *domain.User) (*Session, error) {
	if user.Status == domain.UserStatusSuspended {
		return nil, apperror.Wrap(apperror.CodePermissionDenied, domain.ErrUserSuspended.Error(), domain.ErrUserSuspended).
			WithReason(domain.ReasonUserSuspended)
	}

	accessToken, expiresAt, err := uc.issuer.IssueAccessToken(auth.Identity{
		UserID: user.ID, EmailVerified: user.EmailVerified(), PlatformAdmin: user.PlatformAdmin,
	})
	if err != nil {
		return nil, apperror.Internal("issue access token", err)
	}

	refreshToken := auth.NewOpaqueToken()
	if err := uc.sessions.SaveRefreshToken(ctx, refreshToken, user.ID, uc.issuer.RefreshTTL()); err != nil {
		return nil, apperror.Internal("store refresh token", err)
	}

	return &Session{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt, User: user}, nil
}

// signIn issues the session of a completed sign-in (password, second factor,
// Google or registration; a refresh is not a sign-in) and, for a user with
// platform access, writes it to the audit log. The audit entry is part of
// the sign-in: if it cannot be written the sign-in fails, so a platform
// session never exists unrecorded.
func (uc *IdentityUseCase) signIn(ctx context.Context, user *domain.User) (*Session, error) {
	session, err := uc.issueSession(ctx, user)
	if err != nil {
		return nil, err
	}

	if uc.platformAccess == nil || uc.audit == nil {
		return session, nil
	}

	platformUser, err := uc.platformAccess.HasPlatformAccess(ctx, user.ID)
	if err != nil {
		return nil, apperror.Internal("check platform access", err)
	}

	if platformUser {
		if err := uc.audit.Record(ctx, audit.Entry{
			Actor: user.ID, Action: "platform.signed_in", EntityType: "user", EntityID: user.ID, IP: middleware.ClientIP(ctx),
		}); err != nil {
			return nil, apperror.Internal("audit platform sign-in", err)
		}
	}

	return session, nil
}

// markVerified records the e-mail verification and publishes
// user.email_verified in the same transaction; a no-op when already
// verified.
func (uc *IdentityUseCase) markVerified(ctx context.Context, user *domain.User) error {
	if !user.MarkEmailVerified(uc.now().UTC()) {
		return nil
	}

	return uc.tx.WithTransaction(ctx, platformdb.UserScope(user.ID), func(ctx context.Context) error {
		if err := uc.users.Update(ctx, user); err != nil {
			return err
		}

		if uc.isConfiguredAdmin(user.Email) {
			if _, err := uc.users.GrantPlatformAdmins(ctx, []string{user.Email}); err != nil {
				return err
			}

			user.PlatformAdmin = true
		}

		return uc.events.Publish(ctx, EventUserEmailVerified, "", user.ID, map[string]any{
			"user_id":     user.ID,
			"email":       user.Email,
			"full_name":   user.FullName,
			"locale":      user.Locale.OrDefault(),
			"verified_at": user.EmailVerifiedAt.Format(time.RFC3339),
		})
	})
}

// findUser loads a user, mapping "not found" to notFound.
func (uc *IdentityUseCase) findUser(ctx context.Context, id string) (*domain.User, error) {
	user, err := uc.users.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			return nil, apperror.Wrap(apperror.CodeUnauthenticated, "user not found", err)
		}

		return nil, apperror.Internal("find user", err)
	}

	return user, nil
}

// GrantPlatformAdmins makes the users with these e-mails platform
// admins (PLATFORM_ADMIN_EMAILS at startup), if their e-mail is verified.
func (uc *IdentityUseCase) GrantPlatformAdmins(ctx context.Context, emails []string) error {
	normalized := make([]string, 0, len(emails))

	for _, raw := range emails {
		if addr, err := email.Normalize(raw); err == nil {
			normalized = append(normalized, addr)
		}
	}

	if len(normalized) == 0 {
		return nil
	}

	granted, err := uc.users.GrantPlatformAdmins(ctx, normalized)
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
