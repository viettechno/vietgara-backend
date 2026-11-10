package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestChangePassword(t *testing.T) {
	t.Parallel()

	t.Run("changes it, revokes the other sessions, answers a new one and audits", func(t *testing.T) {
		t.Parallel()

		rec := &fakeAudit{}
		e := newEnv(t, usecase.WithAudit(rec))
		owner := e.verifiedOwner(t)

		other, err := e.uc.Login(t.Context(), "owner@example.com", ownerPassword)
		require.NoError(t, err)

		fresh, err := e.uc.ChangePassword(t.Context(), owner.User.ID, ownerPassword, "brand-new-pass")
		require.NoError(t, err)

		_, err = e.uc.Refresh(t.Context(), owner.RefreshToken)
		requireReason(t, err, domain.ReasonRefreshTokenInvalid)

		_, err = e.uc.Refresh(t.Context(), other.RefreshToken)
		requireReason(t, err, domain.ReasonRefreshTokenInvalid)

		_, err = e.uc.Refresh(t.Context(), fresh.RefreshToken)
		require.NoError(t, err, "the caller keeps a session")

		_, err = e.uc.Login(t.Context(), "owner@example.com", ownerPassword)
		requireReason(t, err, domain.ReasonInvalidCredentials)

		_, err = e.uc.Login(t.Context(), "owner@example.com", "brand-new-pass")
		require.NoError(t, err)

		assert.Equal(t, []string{"user.password_changed"}, rec.actions)
	})

	t.Run("a wrong current password changes nothing", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		_, err := e.uc.ChangePassword(t.Context(), owner.User.ID, "wrong-pass", "brand-new-pass")
		requireReason(t, err, domain.ReasonPasswordIncorrect)
		assertAppError(t, err, apperror.CodePermissionDenied)

		_, err = e.uc.Refresh(t.Context(), owner.RefreshToken)
		require.NoError(t, err, "no session was revoked")

		_, err = e.uc.Login(t.Context(), "owner@example.com", ownerPassword)
		require.NoError(t, err)
	})

	t.Run("the new password follows the sign-up rule", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		_, err := e.uc.ChangePassword(t.Context(), owner.User.ID, ownerPassword, "short")
		requireReason(t, err, apperror.ReasonValidation)

		_, err = e.uc.ChangePassword(t.Context(), owner.User.ID, ownerPassword, string(make([]byte, domain.MaxPasswordBytes+1)))
		requireReason(t, err, apperror.ReasonValidation)

		_, err = e.uc.Login(t.Context(), "owner@example.com", ownerPassword)
		require.NoError(t, err, "the password is unchanged")
	})

	t.Run("a user with no password has nothing to change", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		googleOnly, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g-2", Email: "g@gmail.com", EmailVerified: true, FullName: "G"})
		require.NoError(t, err)

		_, err = e.uc.ChangePassword(t.Context(), googleOnly.User.ID, "anything", "brand-new-pass")
		requireReason(t, err, domain.ReasonNoPassword)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("store failures", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		e.users.updateErr = errBoom
		_, err := e.uc.ChangePassword(t.Context(), owner.User.ID, ownerPassword, "brand-new-pass")
		requireReason(t, err, apperror.ReasonInternal)
	})
}
