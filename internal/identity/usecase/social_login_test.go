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

	acc := session.Account
	assert.Equal(t, domain.AuthProviderGoogle, acc.AuthProvider)
	assert.Equal(t, "new@gmail.com", acc.FullName, "e-mail stands in for a missing name")
	assert.True(t, acc.EmailVerified())
	assert.Equal(t, []string{usecase.EventAccountEmailVerified}, e.events.types)

	again, err := e.uc.GoogleLogin(t.Context(), profile)
	require.NoError(t, err)
	assert.Equal(t, acc.ID, again.Account.ID)
	assert.Len(t, e.events.types, 1, "verified once")
}

func TestGoogleLoginLinksEmailAccount(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	registered := e.register(t)

	session, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g-2", Email: "owner@example.com", EmailVerified: true, FullName: "Minh"})
	require.NoError(t, err)
	assert.Equal(t, registered.Account.ID, session.Account.ID)
	assert.Equal(t, "g-2", *e.accounts.get(registered.Account.ID).GoogleSubject)
	assert.True(t, session.Account.EmailVerified(), "Google verified the address")
}

func TestGoogleLoginFailures(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	_, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "a@b.vn"})
	requireReason(t, err, domain.ReasonInvalidCredentials)

	e.accounts.findErr = errBoom
	_, err = e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "a@b.vn", EmailVerified: true, FullName: "A"})
	requireReason(t, err, apperror.ReasonInternal)

	e.accounts.findErr, e.accounts.createErr = nil, errBoom
	_, err = e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "a@b.vn", EmailVerified: true, FullName: "A"})
	requireReason(t, err, apperror.ReasonInternal)

	e2 := newEnv(t)
	e2.register(t)
	e2.accounts.updateErr = errBoom
	_, err = e2.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "owner@example.com", EmailVerified: true})
	requireReason(t, err, apperror.ReasonInternal)

	e3 := newEnv(t)
	e3.events.err = errBoom
	_, err = e3.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g", Email: "z@b.vn", EmailVerified: true})
	requireReason(t, err, apperror.ReasonInternal)
}
