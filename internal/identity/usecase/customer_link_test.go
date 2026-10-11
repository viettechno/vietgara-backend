package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestFindVerifiedUserIDByEmail(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)

	_, found, err := e.uc.FindVerifiedUserIDByEmail(t.Context(), "owner@example.com")
	require.NoError(t, err)
	assert.False(t, found, "an unverified e-mail is nobody's")

	_, err = e.uc.VerifyEmail(t.Context(), session.User.ID, e.mailer.lastCode(t, "owner@example.com"), session.RefreshToken)
	require.NoError(t, err)

	id, found, err := e.uc.FindVerifiedUserIDByEmail(t.Context(), "OWNER@example.com")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, session.User.ID, id)

	_, found, err = e.uc.FindVerifiedUserIDByEmail(t.Context(), "nobody@example.com")
	require.NoError(t, err)
	assert.False(t, found)

	e.users.findErr = errBoom
	_, _, err = e.uc.FindVerifiedUserIDByEmail(t.Context(), "owner@example.com")
	requireReason(t, err, apperror.ReasonInternal)
}

func TestCustomerLinkCode(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)
	id := session.User.ID
	linkID := "link-1"

	require.NoError(t, e.uc.SendCustomerLinkCode(t.Context(), id, linkID, "Gara Zeta"))
	require.NotEmpty(t, e.mailer.sent)
	assert.Contains(t, e.mailer.sent[len(e.mailer.sent)-1].Subject, "Gara Zeta")

	requireReason(t, e.uc.SendCustomerLinkCode(t.Context(), id, linkID, "Gara Zeta"), domain.ReasonOTPResendTooSoon)

	requireReason(t, e.uc.CheckCustomerLinkCode(t.Context(), linkID, "000000"), domain.ReasonOTPInvalid)
	requireReason(t, e.uc.CheckCustomerLinkCode(t.Context(), "other-link", "000000"), domain.ReasonOTPExpired)

	code := e.mailer.lastCode(t, "owner@example.com")
	require.NoError(t, e.uc.CheckCustomerLinkCode(t.Context(), linkID, code))
	requireReason(t, e.uc.CheckCustomerLinkCode(t.Context(), linkID, code), domain.ReasonOTPExpired)

	requireReason(t, e.uc.SendCustomerLinkCode(t.Context(), "missing", linkID, "Gara Zeta"), apperror.ReasonUnauthenticated)

	e.advance(domain.OTPResendCooldown)

	e.mailer.err = errBoom
	requireReason(t, e.uc.SendCustomerLinkCode(t.Context(), id, linkID, "Gara Zeta"), apperror.ReasonInternal)
}
