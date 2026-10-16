package repository

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
)

// RedisStore keeps refresh tokens, password-reset tokens and OTPs in
// Redis (Memorystore): every instance of the backend sees the same
// sessions and they survive restarts (Security Architecture Section 2.2).
type RedisStore struct {
	client redis.UniversalClient
}

// NewRedisStore returns a RedisStore on client.
func NewRedisStore(client redis.UniversalClient) *RedisStore {
	return &RedisStore{client: client}
}

func refreshKey(token string) string       { return "identity:refresh:" + token }
func userSessionsKey(userID string) string { return "identity:sessions:" + userID }
func resetKey(token string) string         { return "identity:reset:" + token }
func otpKey(key string) string             { return "identity:otp:" + key }
func mfaChallengeKey(token string) string  { return "identity:mfa-challenge:" + token }
func mfaFailuresKey(userID string) string  { return "identity:mfa-failures:" + userID }

// SaveRefreshToken stores token for userID and indexes it by user so
// every session can be revoked.
func (s *RedisStore) SaveRefreshToken(ctx context.Context, token, userID string, ttl time.Duration) error {
	_, err := s.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, refreshKey(token), userID, ttl)
		p.SAdd(ctx, userSessionsKey(userID), token)
		p.Expire(ctx, userSessionsKey(userID), ttl)

		return nil
	})

	return err
}

// RefreshTokenUser returns the user of token.
func (s *RedisStore) RefreshTokenUser(ctx context.Context, token string) (string, error) {
	userID, err := s.client.Get(ctx, refreshKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return "", domain.ErrRefreshTokenInvalid
	}

	return userID, err
}

// RevokeRefreshToken deletes token.
func (s *RedisStore) RevokeRefreshToken(ctx context.Context, token string) error {
	userID, err := s.client.GetDel(ctx, refreshKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}

	if err != nil {
		return err
	}

	return s.client.SRem(ctx, userSessionsKey(userID), token).Err()
}

// RevokeAllRefreshTokens deletes every refresh token of userID.
func (s *RedisStore) RevokeAllRefreshTokens(ctx context.Context, userID string) error {
	tokens, err := s.client.SMembers(ctx, userSessionsKey(userID)).Result()
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(tokens)+1)
	for _, token := range tokens {
		keys = append(keys, refreshKey(token))
	}

	keys = append(keys, userSessionsKey(userID))

	return s.client.Del(ctx, keys...).Err()
}

// SaveResetToken stores a password-reset token.
func (s *RedisStore) SaveResetToken(ctx context.Context, token, userID string, ttl time.Duration) error {
	return s.client.Set(ctx, resetKey(token), userID, ttl).Err()
}

// ConsumeResetToken returns and deletes a password-reset token.
func (s *RedisStore) ConsumeResetToken(ctx context.Context, token string) (string, error) {
	userID, err := s.client.GetDel(ctx, resetKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return "", domain.ErrResetTokenInvalid
	}

	return userID, err
}

// SaveMFAChallenge stores the token of a sign-in waiting for the second factor.
func (s *RedisStore) SaveMFAChallenge(ctx context.Context, token, userID string, ttl time.Duration) error {
	return s.client.Set(ctx, mfaChallengeKey(token), userID, ttl).Err()
}

// MFAChallengeUser returns the user of a live challenge token.
func (s *RedisStore) MFAChallengeUser(ctx context.Context, token string) (string, error) {
	userID, err := s.client.Get(ctx, mfaChallengeKey(token)).Result()
	if errors.Is(err, redis.Nil) {
		return "", domain.ErrMFAChallengeInvalid
	}

	return userID, err
}

// DeleteMFAChallenge removes a challenge token.
func (s *RedisStore) DeleteMFAChallenge(ctx context.Context, token string) error {
	return s.client.Del(ctx, mfaChallengeKey(token)).Err()
}

// RecordMFAFailure counts a wrong code; the count lives until lockout after
// the latest failure.
func (s *RedisStore) RecordMFAFailure(ctx context.Context, userID string, lockout time.Duration) (int, error) {
	pipe := s.client.TxPipeline()
	incr := pipe.Incr(ctx, mfaFailuresKey(userID))
	pipe.Expire(ctx, mfaFailuresKey(userID), lockout)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}

	return int(incr.Val()), nil
}

// MFAFailures returns the wrong codes counted for userID.
func (s *RedisStore) MFAFailures(ctx context.Context, userID string) (int, error) {
	n, err := s.client.Get(ctx, mfaFailuresKey(userID)).Int()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}

	return n, err
}

// ClearMFAFailures forgets the wrong codes of userID.
func (s *RedisStore) ClearMFAFailures(ctx context.Context, userID string) error {
	return s.client.Del(ctx, mfaFailuresKey(userID)).Err()
}

// Get returns the OTP stored under key.
func (s *RedisStore) Get(ctx context.Context, key string) (*domain.OTP, error) {
	fields, err := s.client.HGetAll(ctx, otpKey(key)).Result()
	if err != nil {
		return nil, err
	}

	if len(fields) == 0 {
		return nil, nil //nolint:nilnil // no OTP
	}

	expires, _ := strconv.ParseInt(fields["expires_at"], 10, 64)
	resend, _ := strconv.ParseInt(fields["resend_at"], 10, 64)
	attempts, _ := strconv.Atoi(fields["attempts"])

	return &domain.OTP{
		CodeHash:  fields["code_hash"],
		ExpiresAt: time.UnixMilli(expires).UTC(),
		ResendAt:  time.UnixMilli(resend).UTC(),
		Attempts:  attempts,
	}, nil
}

// Put stores otp until it expires, replacing any previous one.
func (s *RedisStore) Put(ctx context.Context, key string, otp *domain.OTP) error {
	_, err := s.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Del(ctx, otpKey(key))
		p.HSet(ctx, otpKey(key), map[string]any{
			"code_hash":  otp.CodeHash,
			"expires_at": otp.ExpiresAt.UnixMilli(),
			"resend_at":  otp.ResendAt.UnixMilli(),
			"attempts":   otp.Attempts,
		})
		p.PExpireAt(ctx, otpKey(key), otp.ExpiresAt)

		return nil
	})

	return err
}

// IncrementAttempts counts a wrong code atomically.
func (s *RedisStore) IncrementAttempts(ctx context.Context, key string) (int, error) {
	n, err := s.client.HIncrBy(ctx, otpKey(key), "attempts", 1).Result()

	return int(n), err
}

// Delete removes the OTP under key.
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, otpKey(key)).Err()
}

// MemoryStore is the in-process SessionStore and OTPStore used when no
// Redis is configured. It does not survive restarts nor work across
// replicas: local development only.
type MemoryStore struct {
	mu       sync.Mutex
	now      func() time.Time
	refresh  map[string]entry
	resets   map[string]entry
	otps     map[string]*domain.OTP
	sessions map[string]map[string]bool
	// challenges maps an MFA challenge token to its user.
	challenges map[string]entry
	// failures counts wrong second-factor codes per user.
	failures map[string]mfaFailures
}

type mfaFailures struct {
	count   int
	expires time.Time
}

type entry struct {
	value   string
	expires time.Time
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		now: time.Now, refresh: map[string]entry{}, resets: map[string]entry{},
		otps: map[string]*domain.OTP{}, sessions: map[string]map[string]bool{},
		challenges: map[string]entry{}, failures: map[string]mfaFailures{},
	}
}

func (m *MemoryStore) live(e entry, ok bool) (string, bool) {
	if !ok || !m.now().Before(e.expires) {
		return "", false
	}

	return e.value, true
}

// SaveRefreshToken stores token.
func (m *MemoryStore) SaveRefreshToken(_ context.Context, token, userID string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.refresh[token] = entry{value: userID, expires: m.now().Add(ttl)}
	if m.sessions[userID] == nil {
		m.sessions[userID] = map[string]bool{}
	}

	m.sessions[userID][token] = true

	return nil
}

// RefreshTokenUser returns the user of token.
func (m *MemoryStore) RefreshTokenUser(_ context.Context, token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.refresh[token]
	if userID, live := m.live(e, ok); live {
		return userID, nil
	}

	return "", domain.ErrRefreshTokenInvalid
}

// RevokeRefreshToken deletes token.
func (m *MemoryStore) RevokeRefreshToken(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.refresh[token]; ok {
		delete(m.sessions[e.value], token)
		delete(m.refresh, token)
	}

	return nil
}

// RevokeAllRefreshTokens deletes every refresh token of userID.
func (m *MemoryStore) RevokeAllRefreshTokens(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for token := range m.sessions[userID] {
		delete(m.refresh, token)
	}

	delete(m.sessions, userID)

	return nil
}

// SaveResetToken stores a password-reset token.
func (m *MemoryStore) SaveResetToken(_ context.Context, token, userID string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.resets[token] = entry{value: userID, expires: m.now().Add(ttl)}

	return nil
}

// ConsumeResetToken returns and deletes a password-reset token.
func (m *MemoryStore) ConsumeResetToken(_ context.Context, token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.resets[token]
	delete(m.resets, token)

	if userID, live := m.live(e, ok); live {
		return userID, nil
	}

	return "", domain.ErrResetTokenInvalid
}

// SaveMFAChallenge stores the token of a sign-in waiting for the second factor.
func (m *MemoryStore) SaveMFAChallenge(_ context.Context, token, userID string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.challenges[token] = entry{value: userID, expires: m.now().Add(ttl)}

	return nil
}

// MFAChallengeUser returns the user of a live challenge token.
func (m *MemoryStore) MFAChallengeUser(_ context.Context, token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.challenges[token]
	if userID, live := m.live(e, ok); live {
		return userID, nil
	}

	return "", domain.ErrMFAChallengeInvalid
}

// DeleteMFAChallenge removes a challenge token.
func (m *MemoryStore) DeleteMFAChallenge(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.challenges, token)

	return nil
}

// RecordMFAFailure counts a wrong code; the count lives until lockout after
// the latest failure.
func (m *MemoryStore) RecordMFAFailure(_ context.Context, userID string, lockout time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f := m.failures[userID]
	if !m.now().Before(f.expires) {
		f.count = 0
	}

	f.count++
	f.expires = m.now().Add(lockout)
	m.failures[userID] = f

	return f.count, nil
}

// MFAFailures returns the wrong codes counted for userID.
func (m *MemoryStore) MFAFailures(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	f := m.failures[userID]
	if !m.now().Before(f.expires) {
		return 0, nil
	}

	return f.count, nil
}

// ClearMFAFailures forgets the wrong codes of userID.
func (m *MemoryStore) ClearMFAFailures(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.failures, userID)

	return nil
}

// Get returns a copy of the OTP under key.
func (m *MemoryStore) Get(_ context.Context, key string) (*domain.OTP, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	otp, ok := m.otps[key]
	if !ok || otp.Expired(m.now()) {
		return nil, nil //nolint:nilnil // no OTP
	}

	copied := *otp

	return &copied, nil
}

// Put stores otp.
func (m *MemoryStore) Put(_ context.Context, key string, otp *domain.OTP) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	copied := *otp
	m.otps[key] = &copied

	return nil
}

// IncrementAttempts counts a wrong code.
func (m *MemoryStore) IncrementAttempts(_ context.Context, key string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	otp, ok := m.otps[key]
	if !ok {
		return 1, nil
	}

	otp.Attempts++

	return otp.Attempts, nil
}

// Delete removes the OTP under key.
func (m *MemoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.otps, key)

	return nil
}
