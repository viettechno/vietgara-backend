package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestGoogleLogin(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	profile := usecase.GoogleProfile{Subject: "g-1", Email: "New@Gmail.com", EmailVerified: true, FullName: "", Locale: domain.LocaleEN}

	session, err := e.uc.GoogleLogin(t.Context(), profile)
	require.NoError(t, err)

	user := session.User
	assert.Equal(t, domain.AuthProviderGoogle, user.AuthProvider)
	assert.Equal(t, "new@gmail.com", user.FullName, "e-mail stands in for a missing name")
	assert.True(t, user.EmailVerified())
	assert.Equal(t, []string{usecase.EventUserEmailVerified}, e.events.types)

	again, err := e.uc.GoogleLogin(t.Context(), profile)
	require.NoError(t, err)
	assert.Equal(t, user.ID, again.User.ID)
	assert.Len(t, e.events.types, 1, "verified once")
}

func TestGoogleLoginLinksEmailUser(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	registered := e.register(t)

	session, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g-2", Email: "owner@example.com", EmailVerified: true, FullName: "Minh"})
	require.NoError(t, err)
	assert.Equal(t, registered.User.ID, session.User.ID)
	assert.Equal(t, "g-2", *e.users.get(registered.User.ID).GoogleSubject)
	assert.True(t, session.User.EmailVerified(), "Google verified the address")
}

func TestGoogleLoginFailures(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	_, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "a@b.vn"})
	requireReason(t, err, domain.ReasonInvalidCredentials)

	e.users.findErr = errBoom
	_, err = e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "a@b.vn", EmailVerified: true, FullName: "A"})
	requireReason(t, err, apperror.ReasonInternal)

	e.users.findErr, e.users.createErr = nil, errBoom
	_, err = e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "a@b.vn", EmailVerified: true, FullName: "A"})
	requireReason(t, err, apperror.ReasonInternal)

	e2 := newEnv(t)
	e2.register(t)
	e2.users.updateErr = errBoom
	_, err = e2.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "owner@example.com", EmailVerified: true})
	requireReason(t, err, apperror.ReasonInternal)

	e3 := newEnv(t)
	e3.events.err = errBoom
	_, err = e3.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "z@b.vn", EmailVerified: true})
	requireReason(t, err, apperror.ReasonInternal)
}

// TestGoogleNameSource covers FR-IAM-12: a name taken from Google is
// refreshed by a later sign-in, a name the user typed never is.
func TestGoogleNameSource(t *testing.T) {
	t.Parallel()

	t.Run("a Google name is refreshed until the user edits it", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		profile := usecase.GoogleProfile{Subject: "g-3", Email: "an@gmail.com", EmailVerified: true, FullName: "An Nguyen"}

		session, err := e.uc.GoogleLogin(t.Context(), profile)
		require.NoError(t, err)

		id := session.User.ID
		assert.Equal(t, domain.NameSourceGoogle, e.users.get(id).FullNameSource)
		assert.Equal(t, "An Nguyen", e.users.get(id).FullName)

		profile.FullName = "An Van Nguyen"
		_, err = e.uc.GoogleLogin(t.Context(), profile)
		require.NoError(t, err)
		assert.Equal(t, "An Van Nguyen", e.users.get(id).FullName, "refreshed from Google")
		assert.Equal(t, domain.NameSourceGoogle, e.users.get(id).FullNameSource)

		_, err = e.uc.UpdateMe(t.Context(), id, usecase.ProfileUpdate{FullName: new("Nguyen Van An")})
		require.NoError(t, err)
		assert.Equal(t, domain.NameSourceUser, e.users.get(id).FullNameSource)

		profile.FullName = "Someone Else"
		_, err = e.uc.GoogleLogin(t.Context(), profile)
		require.NoError(t, err)
		assert.Equal(t, "Nguyen Van An", e.users.get(id).FullName, "an edited name is never overwritten")
	})

	t.Run("a typed name survives linking Google", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		registered := e.register(t)
		assert.Equal(t, domain.NameSourceUser, e.users.get(registered.User.ID).FullNameSource)

		_, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{
			Subject: "g-4", Email: "owner@example.com", EmailVerified: true, FullName: "Google Name",
		})
		require.NoError(t, err)
		assert.Equal(t, "Minh", e.users.get(registered.User.ID).FullName)
	})

	t.Run("a blank edit keeps the source", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		session, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g-5", Email: "b@gmail.com", EmailVerified: true, FullName: "Binh"})
		require.NoError(t, err)

		_, err = e.uc.UpdateMe(t.Context(), session.User.ID, usecase.ProfileUpdate{FullName: new("Binh")})
		require.NoError(t, err)
		assert.Equal(t, domain.NameSourceGoogle, e.users.get(session.User.ID).FullNameSource, "same name is not an edit")
	})
}
