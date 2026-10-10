package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestProfile(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)
	id := session.User.ID

	me, err := e.uc.GetMe(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, "Minh", me.FullName)

	en := domain.LocaleEN
	updated, err := e.uc.UpdateMe(t.Context(), id, usecase.ProfileUpdate{FullName: new(" Lan  Anh "), Phone: new("+84901112222"), Locale: &en})
	require.NoError(t, err)
	assert.Equal(t, "Lan Anh", updated.FullName)
	assert.Equal(t, "0901112222", *updated.Phone)
	assert.Equal(t, domain.LocaleEN, updated.Locale)

	cleared, err := e.uc.UpdateMe(t.Context(), id, usecase.ProfileUpdate{Phone: new("")})
	require.NoError(t, err)
	assert.Nil(t, cleared.Phone)

	_, err = e.uc.UpdateMe(t.Context(), id, usecase.ProfileUpdate{FullName: new(" ")})
	requireReason(t, err, apperror.ReasonValidation)
	_, err = e.uc.UpdateMe(t.Context(), id, usecase.ProfileUpdate{Phone: new("1")})
	requireReason(t, err, apperror.ReasonValidation)

	other, err := e.uc.Register(t.Context(), usecase.RegisterRequest{Email: "o@b.vn", Password: "secret123", FullName: "O", Phone: new("0903334444")})
	require.NoError(t, err)
	_, err = e.uc.UpdateMe(t.Context(), id, usecase.ProfileUpdate{Phone: new("0903334444")})
	requireReason(t, err, domain.ReasonPhoneTaken)

	_, err = e.uc.UpdateMe(t.Context(), other.User.ID, usecase.ProfileUpdate{Phone: new("0903334444")})
	require.NoError(t, err, "keeping one's own phone")

	_, err = e.uc.UpdateMe(t.Context(), "missing", usecase.ProfileUpdate{})
	requireReason(t, err, apperror.ReasonUnauthenticated)

	e.users.updateErr = domain.ErrPhoneAlreadyExists
	_, err = e.uc.UpdateMe(t.Context(), id, usecase.ProfileUpdate{})
	requireReason(t, err, domain.ReasonPhoneTaken)
}
