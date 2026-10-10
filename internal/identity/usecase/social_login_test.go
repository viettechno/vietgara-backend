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
