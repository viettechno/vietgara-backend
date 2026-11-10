package usecase

import (
	"context"

	"golang.org/x/crypto/bcrypt"

	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ChangePassword replaces the password of a signed-in user after the current
// one (FR-IAM-13). Every refresh token of the user is revoked and the caller
// gets a new session, since a changed password is often the remedy for a lost
// device. A turned-on MFA is not asked again: the user is already signed in.
// A user with no password signs in with Google only and has nothing to change.
func (uc *IdentityUseCase) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) (*Session, error) {
	user, err := uc.userWithPassword(ctx, userID, currentPassword)
	if err != nil {
		return nil, err
	}

	if err := checkPassword("newPassword", newPassword); err != nil {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, apperror.Internal("hash password", err)
	}

	passwordHash := string(hash)
	user.PasswordHash = &passwordHash

	if err := uc.users.Update(ctx, user); err != nil {
		return nil, apperror.Internal("update password", err)
	}

	if err := uc.sessions.RevokeAllRefreshTokens(ctx, user.ID); err != nil {
		return nil, apperror.Internal("revoke sessions", err)
	}

	if uc.audit != nil {
		if err := uc.audit.Record(ctx, audit.Entry{
			Actor: user.ID, Action: "user.password_changed", EntityType: "user", EntityID: user.ID,
		}); err != nil {
			return nil, apperror.Internal("audit password change", err)
		}
	}

	return uc.issueSession(ctx, user)
}
