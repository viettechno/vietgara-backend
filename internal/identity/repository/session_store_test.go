package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/repository"
)

type store interface {
	SaveRefreshToken(ctx context.Context, token, userID string, ttl time.Duration) error
	RefreshTokenUser(ctx context.Context, token string) (string, error)
	RevokeRefreshToken(ctx context.Context, token string) error
	RevokeAllRefreshTokens(ctx context.Context, userID string) error
	SaveResetToken(ctx context.Context, token, userID string, ttl time.Duration) error
	ConsumeResetToken(ctx context.Context, token string) (string, error)
	Get(ctx context.Context, key string) (*domain.OTP, error)
	Put(ctx context.Context, key string, otp *domain.OTP) error
	IncrementAttempts(ctx context.Context, key string) (int, error)
	Delete(ctx context.Context, key string) error
	SaveMFAChallenge(ctx context.Context, token, userID string, ttl time.Duration) error
	MFAChallengeUser(ctx context.Context, token string) (string, error)
	DeleteMFAChallenge(ctx context.Context, token string) error
	RecordMFAFailure(ctx context.Context, userID string, lockout time.Duration) (int, error)
	MFAFailures(ctx context.Context, userID string) (int, error)
	ClearMFAFailures(ctx context.Context, userID string) error
}

func stores(t *testing.T) map[string]store {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	return map[string]store{"redis": repository.NewRedisStore(client), "memory": repository.NewMemoryStore()}
}

func TestSessionStores(t *testing.T) {
	t.Parallel()

	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			require.NoError(t, s.SaveRefreshToken(ctx, "r1", "user", time.Hour))
			require.NoError(t, s.SaveRefreshToken(ctx, "r2", "user", time.Hour))

			id, err := s.RefreshTokenUser(ctx, "r1")
			require.NoError(t, err)
			assert.Equal(t, "user", id)

			require.NoError(t, s.RevokeRefreshToken(ctx, "r1"))
			require.NoError(t, s.RevokeRefreshToken(ctx, "r1"), "revoking twice is fine")
			_, err = s.RefreshTokenUser(ctx, "r1")
			require.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)

			require.NoError(t, s.RevokeAllRefreshTokens(ctx, "user"))
			_, err = s.RefreshTokenUser(ctx, "r2")
			require.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)

			require.NoError(t, s.SaveResetToken(ctx, "reset", "user", time.Minute))
			id, err = s.ConsumeResetToken(ctx, "reset")
			require.NoError(t, err)
			assert.Equal(t, "user", id)

			_, err = s.ConsumeResetToken(ctx, "reset")
			require.ErrorIs(t, err, domain.ErrResetTokenInvalid)
		})
	}
}

func TestOTPStores(t *testing.T) {
	t.Parallel()

	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			got, err := s.Get(ctx, "k")
			require.NoError(t, err)
			assert.Nil(t, got)

			n, err := s.IncrementAttempts(ctx, "missing")
			require.NoError(t, err)
			assert.Equal(t, 1, n)
			require.NoError(t, s.Delete(ctx, "missing"))

			now := time.Now().UTC().Truncate(time.Millisecond)
			otp := &domain.OTP{CodeHash: "h", ExpiresAt: now.Add(time.Minute), ResendAt: now.Add(time.Second)}
			require.NoError(t, s.Put(ctx, "k", otp))

			n, err = s.IncrementAttempts(ctx, "k")
			require.NoError(t, err)
			assert.Equal(t, 1, n)

			got, err = s.Get(ctx, "k")
			require.NoError(t, err)
			assert.Equal(t, &domain.OTP{CodeHash: "h", ExpiresAt: otp.ExpiresAt, ResendAt: otp.ResendAt, Attempts: 1}, got)

			require.NoError(t, s.Delete(ctx, "k"))
			got, err = s.Get(ctx, "k")
			require.NoError(t, err)
			assert.Nil(t, got)
		})
	}
}

func TestMemoryStoreExpiry(t *testing.T) {
	t.Parallel()

	s := repository.NewMemoryStore()
	ctx := t.Context()

	require.NoError(t, s.SaveRefreshToken(ctx, "r", "user", -time.Second))
	_, err := s.RefreshTokenUser(ctx, "r")
	require.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)

	require.NoError(t, s.Put(ctx, "k", &domain.OTP{ExpiresAt: time.Now().Add(-time.Second)}))
	got, err := s.Get(ctx, "k")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestRedisStoreErrors(t *testing.T) {
	t.Parallel()

	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 50 * time.Millisecond})

	t.Cleanup(func() { _ = client.Close() })

	s := repository.NewRedisStore(client)
	ctx := t.Context()

	_, err := s.RefreshTokenUser(ctx, "x")
	require.Error(t, err)
	require.Error(t, s.RevokeRefreshToken(ctx, "x"))
	require.Error(t, s.RevokeAllRefreshTokens(ctx, "x"))
	_, err = s.ConsumeResetToken(ctx, "x")
	require.Error(t, err)
	_, err = s.Get(ctx, "x")
	require.Error(t, err)
}

func TestMFAChallengeAndFailures(t *testing.T) {
	t.Parallel()

	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			_, err := s.MFAChallengeUser(ctx, "c1")
			require.ErrorIs(t, err, domain.ErrMFAChallengeInvalid)

			require.NoError(t, s.SaveMFAChallenge(ctx, "c1", "user", time.Minute))

			id, err := s.MFAChallengeUser(ctx, "c1")
			require.NoError(t, err)
			assert.Equal(t, "user", id)

			require.NoError(t, s.DeleteMFAChallenge(ctx, "c1"))
			require.NoError(t, s.DeleteMFAChallenge(ctx, "c1"), "deleting twice is fine")
			_, err = s.MFAChallengeUser(ctx, "c1")
			require.ErrorIs(t, err, domain.ErrMFAChallengeInvalid)

			n, err := s.MFAFailures(ctx, "user")
			require.NoError(t, err)
			assert.Zero(t, n)

			for want := 1; want <= 3; want++ {
				n, err = s.RecordMFAFailure(ctx, "user", time.Minute)
				require.NoError(t, err)
				assert.Equal(t, want, n)
			}

			n, err = s.MFAFailures(ctx, "user")
			require.NoError(t, err)
			assert.Equal(t, 3, n)

			n, err = s.MFAFailures(ctx, "other")
			require.NoError(t, err)
			assert.Zero(t, n, "failures are per user")

			require.NoError(t, s.ClearMFAFailures(ctx, "user"))
			n, err = s.MFAFailures(ctx, "user")
			require.NoError(t, err)
			assert.Zero(t, n)
		})
	}
}

func TestMFAChallengeExpires(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	s := repository.NewRedisStore(client)
	ctx := t.Context()

	require.NoError(t, s.SaveMFAChallenge(ctx, "c1", "user", time.Minute))
	_, err := s.RecordMFAFailure(ctx, "user", time.Minute)
	require.NoError(t, err)

	mr.FastForward(2 * time.Minute)

	_, err = s.MFAChallengeUser(ctx, "c1")
	require.ErrorIs(t, err, domain.ErrMFAChallengeInvalid)

	n, err := s.MFAFailures(ctx, "user")
	require.NoError(t, err)
	assert.Zero(t, n, "the lockout ends")
}
