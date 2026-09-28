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
	SaveRefreshToken(ctx context.Context, token, accountID string, ttl time.Duration) error
	RefreshTokenAccount(ctx context.Context, token string) (string, error)
	RevokeRefreshToken(ctx context.Context, token string) error
	RevokeAllRefreshTokens(ctx context.Context, accountID string) error
	SaveResetToken(ctx context.Context, token, accountID string, ttl time.Duration) error
	ConsumeResetToken(ctx context.Context, token string) (string, error)
	Get(ctx context.Context, key string) (*domain.OTP, error)
	Put(ctx context.Context, key string, otp *domain.OTP) error
	IncrementAttempts(ctx context.Context, key string) (int, error)
	Delete(ctx context.Context, key string) error
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

			require.NoError(t, s.SaveRefreshToken(ctx, "r1", "acc", time.Hour))
			require.NoError(t, s.SaveRefreshToken(ctx, "r2", "acc", time.Hour))

			id, err := s.RefreshTokenAccount(ctx, "r1")
			require.NoError(t, err)
			assert.Equal(t, "acc", id)

			require.NoError(t, s.RevokeRefreshToken(ctx, "r1"))
			require.NoError(t, s.RevokeRefreshToken(ctx, "r1"), "revoking twice is fine")
			_, err = s.RefreshTokenAccount(ctx, "r1")
			require.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)

			require.NoError(t, s.RevokeAllRefreshTokens(ctx, "acc"))
			_, err = s.RefreshTokenAccount(ctx, "r2")
			require.ErrorIs(t, err, domain.ErrRefreshTokenInvalid)

			require.NoError(t, s.SaveResetToken(ctx, "reset", "acc", time.Minute))
			id, err = s.ConsumeResetToken(ctx, "reset")
			require.NoError(t, err)
			assert.Equal(t, "acc", id)

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

	require.NoError(t, s.SaveRefreshToken(ctx, "r", "acc", -time.Second))
	_, err := s.RefreshTokenAccount(ctx, "r")
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

	_, err := s.RefreshTokenAccount(ctx, "x")
	require.Error(t, err)
	require.Error(t, s.RevokeRefreshToken(ctx, "x"))
	require.Error(t, s.RevokeAllRefreshTokens(ctx, "x"))
	_, err = s.ConsumeResetToken(ctx, "x")
	require.Error(t, err)
	_, err = s.Get(ctx, "x")
	require.Error(t, err)
}
