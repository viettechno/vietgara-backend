package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const ownerPassword = "secret123"

// verifiedOwner registers owner@example.com and verifies the e-mail.
func (e *env) verifiedOwner(t *testing.T) *usecase.Session {
	t.Helper()

	registered := e.register(t)

	session, err := e.uc.VerifyEmail(t.Context(), registered.User.ID, e.mailer.lastCode(t, "owner@example.com"), registered.RefreshToken)
	require.NoError(t, err)

	return session
}

// assertAppError checks the error class that decides the HTTP status.
func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	assert.Equal(t, code, appErr.Code)
}

func googleProfile(subject string) usecase.GoogleProfile {
	return usecase.GoogleProfile{Subject: subject, Email: "owner@gmail.com", EmailVerified: true, FullName: "Minh"}
}

func TestLinkGoogleByIDToken(t *testing.T) {
	t.Parallel()

	t.Run("links, and linking the same account again changes nothing", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		user, err := e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("g-1"))
		require.NoError(t, err)
		assert.True(t, user.GoogleLinked())
		assert.Equal(t, "g-1", *e.users.get(owner.User.ID).GoogleSubject)
		assert.True(t, e.users.get(owner.User.ID).HasPassword(), "the password stays")

		again, err := e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("g-1"))
		require.NoError(t, err)
		assert.Equal(t, user.ID, again.ID)

		signedIn, err := e.uc.GoogleLogin(t.Context(), googleProfile("g-1"))
		require.NoError(t, err)
		assert.Equal(t, owner.User.ID, signedIn.User.ID, "Google now signs in to this user")
	})

	t.Run("refuses a wrong password and a user without one", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		_, err := e.uc.LinkGoogle(t.Context(), owner.User.ID, "wrong-pass", googleProfile("g-1"))
		requireReason(t, err, domain.ReasonPasswordIncorrect)
		assertAppError(t, err, apperror.CodePermissionDenied)
		assert.Nil(t, e.users.get(owner.User.ID).GoogleSubject)

		googleOnly, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g-2", Email: "g@gmail.com", EmailVerified: true, FullName: "G"})
		require.NoError(t, err)

		_, err = e.uc.LinkGoogle(t.Context(), googleOnly.User.ID, "anything", googleProfile("g-3"))
		requireReason(t, err, domain.ReasonNoPassword)
	})

	t.Run("refuses a Google account of another user and a second one for the same user", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		_, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "taken", Email: "other@gmail.com", EmailVerified: true, FullName: "O"})
		require.NoError(t, err)

		_, err = e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("taken"))
		requireReason(t, err, domain.ReasonGoogleAlreadyLinked)
		assertAppError(t, err, apperror.CodeAlreadyExists)

		_, err = e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("mine"))
		require.NoError(t, err)

		_, err = e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("another"))
		requireReason(t, err, domain.ReasonGoogleLinkExists)
	})

	t.Run("needs a verified Google e-mail", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		unverified := googleProfile("g-1")
		unverified.EmailVerified = false

		_, err := e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, unverified)
		requireReason(t, err, domain.ReasonInvalidCredentials)

		_, err = e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, usecase.GoogleProfile{EmailVerified: true})
		requireReason(t, err, domain.ReasonInvalidCredentials)
	})

	t.Run("store failures", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		e.users.updateErr = errBoom
		_, err := e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("g-1"))
		requireReason(t, err, apperror.ReasonInternal)

		e.users.updateErr, e.users.findErr = nil, errBoom
		_, err = e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("g-1"))
		requireReason(t, err, apperror.ReasonInternal)
	})
}

func TestLinkGoogleByPopupProof(t *testing.T) {
	t.Parallel()

	t.Run("a proof links once, and a wrong password does not burn it", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		proof, err := e.uc.CreateGoogleLinkProof(t.Context(), googleProfile("g-1"))
		require.NoError(t, err)

		_, err = e.uc.LinkGoogleWithProof(t.Context(), owner.User.ID, "wrong-pass", proof)
		requireReason(t, err, domain.ReasonPasswordIncorrect)

		user, err := e.uc.LinkGoogleWithProof(t.Context(), owner.User.ID, ownerPassword, proof)
		require.NoError(t, err)
		assert.True(t, user.GoogleLinked())

		_, err = e.uc.LinkGoogleWithProof(t.Context(), owner.User.ID, ownerPassword, proof)
		requireReason(t, err, domain.ReasonLinkProofInvalid)
	})

	t.Run("an unknown or unverified proof is refused", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		_, err := e.uc.LinkGoogleWithProof(t.Context(), owner.User.ID, ownerPassword, "never-issued")
		requireReason(t, err, domain.ReasonLinkProofInvalid)

		unverified := googleProfile("g-1")
		unverified.EmailVerified = false

		_, err = e.uc.CreateGoogleLinkProof(t.Context(), unverified)
		requireReason(t, err, domain.ReasonInvalidCredentials)
	})

	t.Run("a proof of another user's Google account is refused", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		_, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "taken", Email: "other@gmail.com", EmailVerified: true, FullName: "O"})
		require.NoError(t, err)

		proof, err := e.uc.CreateGoogleLinkProof(t.Context(), googleProfile("taken"))
		require.NoError(t, err)

		_, err = e.uc.LinkGoogleWithProof(t.Context(), owner.User.ID, ownerPassword, proof)
		requireReason(t, err, domain.ReasonGoogleAlreadyLinked)
	})
}

func TestUnlinkGoogle(t *testing.T) {
	t.Parallel()

	t.Run("unlinks, revokes the other sessions and answers a new one", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		_, err := e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("g-1"))
		require.NoError(t, err)

		viaGoogle, err := e.uc.GoogleLogin(t.Context(), googleProfile("g-1"))
		require.NoError(t, err)

		fresh, err := e.uc.UnlinkGoogle(t.Context(), owner.User.ID, ownerPassword)
		require.NoError(t, err)
		assert.False(t, fresh.User.GoogleLinked())
		assert.Nil(t, e.users.get(owner.User.ID).GoogleSubject)

		_, err = e.uc.Refresh(t.Context(), viaGoogle.RefreshToken)
		requireReason(t, err, domain.ReasonRefreshTokenInvalid)

		_, err = e.uc.Refresh(t.Context(), owner.RefreshToken)
		requireReason(t, err, domain.ReasonRefreshTokenInvalid)

		_, err = e.uc.Refresh(t.Context(), fresh.RefreshToken)
		require.NoError(t, err, "the caller keeps a session")

		_, err = e.uc.Login(t.Context(), "owner@example.com", ownerPassword)
		require.NoError(t, err, "the password still signs in")
	})

	t.Run("turns Google sign-in off until the user links again", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)
		sameEmail := usecase.GoogleProfile{Subject: "g-1", Email: "owner@example.com", EmailVerified: true, FullName: "Minh"}

		_, err := e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, sameEmail)
		require.NoError(t, err)

		_, err = e.uc.UnlinkGoogle(t.Context(), owner.User.ID, ownerPassword)
		require.NoError(t, err)
		assert.True(t, e.users.get(owner.User.ID).GoogleSigninBlocked)

		_, err = e.uc.GoogleLogin(t.Context(), sameEmail)
		requireReason(t, err, domain.ReasonGoogleSigninBlocked)
		assertAppError(t, err, apperror.CodePermissionDenied)
		assert.Nil(t, e.users.get(owner.User.ID).GoogleSubject, "the e-mail match does not link it again")

		_, err = e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, sameEmail)
		require.NoError(t, err, "the user can link again")
		assert.False(t, e.users.get(owner.User.ID).GoogleSigninBlocked)

		signedIn, err := e.uc.GoogleLogin(t.Context(), sameEmail)
		require.NoError(t, err)
		assert.Equal(t, owner.User.ID, signedIn.User.ID)
	})

	t.Run("refuses without a link, with a wrong password, and for a user with no password", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		_, err := e.uc.UnlinkGoogle(t.Context(), owner.User.ID, ownerPassword)
		requireReason(t, err, domain.ReasonGoogleNotLinked)

		_, err = e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("g-1"))
		require.NoError(t, err)

		_, err = e.uc.UnlinkGoogle(t.Context(), owner.User.ID, "wrong-pass")
		requireReason(t, err, domain.ReasonPasswordIncorrect)
		assert.NotNil(t, e.users.get(owner.User.ID).GoogleSubject, "nothing was removed")

		googleOnly, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g-2", Email: "g@gmail.com", EmailVerified: true, FullName: "G"})
		require.NoError(t, err)

		_, err = e.uc.UnlinkGoogle(t.Context(), googleOnly.User.ID, "anything")
		requireReason(t, err, domain.ReasonNoPassword)
	})

	t.Run("store failures", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		owner := e.verifiedOwner(t)

		_, err := e.uc.LinkGoogle(t.Context(), owner.User.ID, ownerPassword, googleProfile("g-1"))
		require.NoError(t, err)

		e.users.updateErr = errBoom
		_, err = e.uc.UnlinkGoogle(t.Context(), owner.User.ID, ownerPassword)
		requireReason(t, err, apperror.ReasonInternal)
	})
}
