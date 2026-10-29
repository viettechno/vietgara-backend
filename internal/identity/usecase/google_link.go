package usecase

import (
	"context"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// GoogleLinkProofTTL is how long the proof of the Google popup can be turned
// into a link.
const GoogleLinkProofTTL = 5 * time.Minute

// CreateGoogleLinkProof is the web popup's callback (FR-IAM-06): the owner of
// the Google account proved it, and the proof is kept for GoogleLinkProofTTL
// for the signed-in user to claim with its password. The popup posts it to
// the console window that opened it and to nobody else.
func (uc *IdentityUseCase) CreateGoogleLinkProof(ctx context.Context, profile GoogleProfile) (string, error) {
	if profile.Subject == "" || !profile.EmailVerified {
		return "", apperror.New(apperror.CodeUnauthenticated, "Google did not provide a verified e-mail address").
			WithReason(domain.ReasonInvalidCredentials)
	}

	proof := auth.NewOpaqueToken()
	if err := uc.sessions.SaveGoogleLinkProof(ctx, proof, profile.Subject, GoogleLinkProofTTL); err != nil {
		return "", apperror.Internal("store google link proof", err)
	}

	return proof, nil
}

// LinkGoogleWithProof links the Google account of a popup proof to the user
// (web consoles). The password is checked first, so a typo does not burn the
// proof.
func (uc *IdentityUseCase) LinkGoogleWithProof(ctx context.Context, userID, password, proof string) (*domain.User, error) {
	user, err := uc.userWithPassword(ctx, userID, password)
	if err != nil {
		return nil, err
	}

	subject, err := uc.sessions.ConsumeGoogleLinkProof(ctx, proof)
	if err != nil {
		if errors.Is(err, domain.ErrLinkProofInvalid) {
			return nil, apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(domain.ReasonLinkProofInvalid)
		}

		return nil, apperror.Internal("read google link proof", err)
	}

	return uc.linkGoogle(ctx, user, subject)
}

// LinkGoogle links the Google account of a verified ID token to the user
// (mobile apps).
func (uc *IdentityUseCase) LinkGoogle(ctx context.Context, userID, password string, profile GoogleProfile) (*domain.User, error) {
	if profile.Subject == "" || !profile.EmailVerified {
		// Not a 401: the caller is signed in, and a client takes a 401 for an expired session.
		return nil, apperror.New(apperror.CodeInvalidArgument, "Google did not provide a verified e-mail address").
			WithReason(domain.ReasonInvalidCredentials)
	}

	user, err := uc.userWithPassword(ctx, userID, password)
	if err != nil {
		return nil, err
	}

	return uc.linkGoogle(ctx, user, profile.Subject)
}

// UnlinkGoogle removes the user's Google account. The last way to sign in is
// kept: a user with no password is refused. Every refresh token of the user
// is revoked, since unlinking is the remedy for a compromised Google account,
// and the caller gets a new session.
func (uc *IdentityUseCase) UnlinkGoogle(ctx context.Context, userID, password string) (*Session, error) {
	user, err := uc.userWithPassword(ctx, userID, password)
	if err != nil {
		return nil, err
	}

	if !user.GoogleLinked() {
		return nil, googleLinkError(domain.ErrGoogleNotLinked)
	}

	// The sessions go first: once the link is gone a retry is refused, so a
	// failure after the update must not leave a session of the old account.
	if err := uc.sessions.RevokeAllRefreshTokens(ctx, user.ID); err != nil {
		return nil, apperror.Internal("revoke sessions", err)
	}

	user.GoogleSubject = nil
	user.GoogleSigninBlocked = true

	if err := uc.users.Update(ctx, user); err != nil {
		return nil, apperror.Internal("unlink google", err)
	}

	if err := uc.recordGoogleLink(ctx, user.ID, "user.google_unlinked", false); err != nil {
		return nil, apperror.Internal("audit google unlink", err)
	}

	return uc.issueSession(ctx, user)
}

// linkGoogle sets the Google subject of user. Linking the account that is
// already linked to the user changes nothing.
func (uc *IdentityUseCase) linkGoogle(ctx context.Context, user *domain.User, subject string) (*domain.User, error) {
	other, err := uc.users.FindByGoogleSubject(ctx, subject)

	switch {
	case err == nil && other.ID == user.ID:
		return user, nil
	case err == nil:
		return nil, googleLinkError(domain.ErrGoogleAlreadyLinked)
	case !errors.Is(err, domain.ErrUserNotFound):
		return nil, apperror.Internal("find user by google subject", err)
	}

	if user.GoogleLinked() {
		return nil, googleLinkError(domain.ErrGoogleLinkExists)
	}

	user.GoogleSubject = &subject
	user.GoogleSigninBlocked = false

	if err := uc.users.Update(ctx, user); err != nil {
		if errors.Is(err, domain.ErrGoogleAlreadyLinked) {
			return nil, googleLinkError(err) // linked to someone else since the check above
		}

		return nil, apperror.Internal("link google", err)
	}

	if err := uc.recordGoogleLink(ctx, user.ID, "user.google_linked", true); err != nil {
		return nil, apperror.Internal("audit google link", err)
	}

	return user, nil
}

// userWithPassword loads the user and checks the password it confirms with.
// A user with no password signs in with Google only.
func (uc *IdentityUseCase) userWithPassword(ctx context.Context, userID, password string) (*domain.User, error) {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if !user.HasPassword() {
		return nil, googleLinkError(domain.ErrNoPassword)
	}

	if bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(password)) != nil {
		return nil, googleLinkError(domain.ErrPasswordIncorrect)
	}

	return user, nil
}

func (uc *IdentityUseCase) recordGoogleLink(ctx context.Context, userID, action string, linked bool) error {
	if uc.audit == nil {
		return nil
	}

	return uc.audit.Record(ctx, audit.Entry{
		Actor: userID, Action: action, EntityType: "user", EntityID: userID, After: map[string]bool{"googleLinked": linked},
	})
}

// googleLinkError gives each rule of linking its code: a wrong password is a
// 403, not a 401, so that a client does not take it for an expired session.
func googleLinkError(err error) error {
	switch {
	case errors.Is(err, domain.ErrPasswordIncorrect):
		return apperror.Wrap(apperror.CodePermissionDenied, err.Error(), err).WithReason(domain.ReasonPasswordIncorrect)
	case errors.Is(err, domain.ErrNoPassword):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(domain.ReasonNoPassword)
	case errors.Is(err, domain.ErrGoogleAlreadyLinked):
		return apperror.Wrap(apperror.CodeAlreadyExists, err.Error(), err).WithReason(domain.ReasonGoogleAlreadyLinked)
	case errors.Is(err, domain.ErrGoogleSigninBlocked):
		return apperror.Wrap(apperror.CodePermissionDenied, err.Error(), err).WithReason(domain.ReasonGoogleSigninBlocked)
	case errors.Is(err, domain.ErrGoogleLinkExists):
		return apperror.Wrap(apperror.CodeAlreadyExists, err.Error(), err).WithReason(domain.ReasonGoogleLinkExists)
	default:
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(domain.ReasonGoogleNotLinked)
	}
}
