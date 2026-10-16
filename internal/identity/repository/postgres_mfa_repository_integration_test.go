//go:build integration

package repository_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// TestMFARepository runs the whole life of a second factor as the
// RLS-bound application role: pending enrolment, confirmation, replay
// protection, single-use recovery codes and removal.
func TestMFARepository(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	pool := pg.AppPool(t)
	users, mfa := repository.NewPostgresUserRepository(pool), repository.NewPostgresMFARepository(pool)

	userID := uuid.NewString()
	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'mfa@example.com', 'Mfa')`, userID)
	require.NoError(t, err)

	_, err = mfa.Find(ctx, userID)
	require.ErrorIs(t, err, domain.ErrMFANotFound)

	ok, err := mfa.SavePending(ctx, userID, []byte("sealed-1"))
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = mfa.SavePending(ctx, userID, []byte("sealed-2"))
	require.NoError(t, err)
	require.True(t, ok, "a pending enrolment is replaced")

	pending, err := mfa.Find(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, []byte("sealed-2"), pending.SecretEncrypted)
	assert.False(t, pending.Enabled())

	user, err := users.FindByID(ctx, userID)
	require.NoError(t, err)
	assert.False(t, user.MFAEnabled, "pending is not enabled")

	enabledAt := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, mfa.Enable(ctx, userID, enabledAt, 100, []string{"h1", "h2", "h3"}))
	require.ErrorIs(t, mfa.Enable(ctx, userID, enabledAt, 100, nil), domain.ErrMFAEnrollmentNotStarted, "already enabled")

	ok, err = mfa.SavePending(ctx, userID, []byte("sealed-3"))
	require.NoError(t, err)
	assert.False(t, ok, "an enabled factor is never replaced")

	user, err = users.FindByID(ctx, userID)
	require.NoError(t, err)
	assert.True(t, user.MFAEnabled)

	enabled, err := mfa.Find(ctx, userID)
	require.NoError(t, err)
	assert.True(t, enabled.Enabled())
	assert.EqualValues(t, 100, enabled.LastUsedStep)

	ok, err = mfa.AdvanceStep(ctx, userID, 100)
	require.NoError(t, err)
	assert.False(t, ok, "the step of the confirming code is spent")

	ok, err = mfa.AdvanceStep(ctx, userID, 101)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = mfa.AdvanceStep(ctx, userID, 101)
	require.NoError(t, err)
	assert.False(t, ok, "replay")

	n, err := mfa.RecoveryCodesRemaining(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	ok, err = mfa.UseRecoveryCode(ctx, userID, "h1")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = mfa.UseRecoveryCode(ctx, userID, "h1")
	require.NoError(t, err)
	assert.False(t, ok, "a recovery code works once")

	ok, err = mfa.UseRecoveryCode(ctx, userID, "nope")
	require.NoError(t, err)
	assert.False(t, ok)

	n, err = mfa.RecoveryCodesRemaining(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	require.NoError(t, mfa.ReplaceRecoveryCodes(ctx, userID, []string{"h1"}))

	n, err = mfa.RecoveryCodesRemaining(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "regeneration replaces the old set, spent codes included")

	require.NoError(t, mfa.Delete(ctx, userID))

	_, err = mfa.Find(ctx, userID)
	require.ErrorIs(t, err, domain.ErrMFANotFound)

	n, err = mfa.RecoveryCodesRemaining(ctx, userID)
	require.NoError(t, err)
	assert.Zero(t, n)

	user, err = users.FindByID(ctx, userID)
	require.NoError(t, err)
	assert.False(t, user.MFAEnabled)
}
