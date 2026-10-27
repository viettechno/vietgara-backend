package usecase_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

type signInAudit struct {
	entries []audit.Entry
	err     error
}

func (a *signInAudit) Record(_ context.Context, e audit.Entry) error {
	if a.err != nil {
		return a.err
	}

	a.entries = append(a.entries, e)

	return nil
}

type platformUsers struct {
	ids []string
	err error
}

func (p platformUsers) HasPlatformAccess(_ context.Context, userID string) (bool, error) {
	return slices.Contains(p.ids, userID), p.err
}

// withPlatformAudit registers the owner, then swaps in a use case that audits
// the sign-ins of the users platform reports as platform users.
func withPlatformAudit(t *testing.T, platform func(userID string) usecase.PlatformAccessChecker) (*env, *signInAudit, string) {
	t.Helper()

	e := newEnv(t)
	userID := e.register(t).User.ID
	rec := &signInAudit{}
	e.uc = usecase.NewIdentityUseCase(e.users, e.store, e.store, e.mailer, e.events, passTx{}, e.issuer,
		usecase.WithClock(func() time.Time { return *e.clock }), usecase.WithAudit(rec), usecase.WithPlatformSignInAudit(platform(userID)))

	return e, rec, userID
}

func TestPlatformSignInIsAudited(t *testing.T) {
	t.Parallel()

	e, rec, userID := withPlatformAudit(t, func(id string) usecase.PlatformAccessChecker { return platformUsers{ids: []string{id}} })

	session, err := e.uc.Login(t.Context(), "owner@example.com", "secret123")
	require.NoError(t, err)
	require.Len(t, rec.entries, 1)
	assert.Equal(t, "platform.signed_in", rec.entries[0].Action)
	assert.Equal(t, userID, rec.entries[0].Actor)
	assert.Empty(t, rec.entries[0].GarageID, "a platform entry belongs to no garage")

	// A refresh is not a sign-in.
	_, err = e.uc.Refresh(t.Context(), session.RefreshToken)
	require.NoError(t, err)
	assert.Len(t, rec.entries, 1)
}

func TestSignInOfAUserWithoutPlatformAccessLeavesNoEntry(t *testing.T) {
	t.Parallel()

	e, rec, _ := withPlatformAudit(t, func(string) usecase.PlatformAccessChecker { return platformUsers{} })

	_, err := e.uc.Login(t.Context(), "owner@example.com", "secret123")
	require.NoError(t, err)
	assert.Empty(t, rec.entries)
}

func TestPlatformSignInFailsWhenItCannotBeRecorded(t *testing.T) {
	t.Parallel()

	e, rec, _ := withPlatformAudit(t, func(id string) usecase.PlatformAccessChecker { return platformUsers{ids: []string{id}} })
	rec.err = errBoom

	_, err := e.uc.Login(t.Context(), "owner@example.com", "secret123")
	requireReason(t, err, "INTERNAL")

	failing, _, _ := withPlatformAudit(t, func(string) usecase.PlatformAccessChecker { return platformUsers{err: errBoom} })
	_, err = failing.uc.Login(t.Context(), "owner@example.com", "secret123")
	requireReason(t, err, "INTERNAL")
}
