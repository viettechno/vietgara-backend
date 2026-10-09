package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestLogin(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	registered := e.register(t)

	session, err := e.uc.Login(t.Context(), " OWNER@example.com", "secret123")
	require.NoError(t, err)
	assert.Equal(t, registered.User.ID, session.User.ID)

	for _, tc := range [][2]string{{"owner@example.com", "wrong-pass"}, {"nobody@example.com", "secret123"}, {"bad", "x"}, {"owner@example.com", ""}} {
		_, err := e.uc.Login(t.Context(), tc[0], tc[1])
		requireReason(t, err, domain.ReasonInvalidCredentials)
	}

	user := e.users.get(registered.User.ID)
	user.Status = domain.UserStatusSuspended
	_, err = e.uc.Login(t.Context(), "owner@example.com", "secret123")
	requireReason(t, err, domain.ReasonUserSuspended)

	e.users.findErr = errBoom
	_, err = e.uc.Login(t.Context(), "owner@example.com", "secret123")
	requireReason(t, err, apperror.ReasonInternal)
}

func TestRefreshAndLogout(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	first := e.register(t)

	second, err := e.uc.Refresh(t.Context(), first.RefreshToken)
	require.NoError(t, err)
	assert.NotEqual(t, first.RefreshToken, second.RefreshToken)

	_, err = e.uc.Refresh(t.Context(), first.RefreshToken)
	requireReason(t, err, domain.ReasonRefreshTokenInvalid)

	require.NoError(t, e.uc.Logout(t.Context(), second.RefreshToken))
	_, err = e.uc.Refresh(t.Context(), second.RefreshToken)
	requireReason(t, err, domain.ReasonRefreshTokenInvalid)

	third, err := e.uc.Login(t.Context(), "owner@example.com", "secret123")
	require.NoError(t, err)

	delete(e.users.byID, third.User.ID)
	_, err = e.uc.Refresh(t.Context(), third.RefreshToken)
	requireReason(t, err, apperror.ReasonUnauthenticated)
}
