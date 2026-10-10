package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
)

// DefaultCacheTTL bounds how stale a cached entry can get if an
// invalidation is ever lost.
const DefaultCacheTTL = 5 * time.Minute

// CacheKey is the entitlement cache key of an owner (LLD Section 3.3).
func CacheKey(userID string) string {
	return "license:entitlement:" + userID
}

// RedisEntitlementCache stores entitlements as JSON in Redis (Memorystore),
// shared by every backend instance.
type RedisEntitlementCache struct {
	client redis.UniversalClient
	ttl    time.Duration
}

// NewRedisEntitlementCache returns a cache over client with the given TTL.
func NewRedisEntitlementCache(client redis.UniversalClient, ttl time.Duration) *RedisEntitlementCache {
	return &RedisEntitlementCache{client: client, ttl: ttl}
}

// Get returns the cached entitlements, or (nil, nil) on a miss.
func (c *RedisEntitlementCache) Get(ctx context.Context, userID string) (*domain.Entitlements, error) {
	raw, err := c.client.Get(ctx, CacheKey(userID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil //nolint:nilnil // cache miss
	}

	if err != nil {
		return nil, err
	}

	var e domain.Entitlements
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}

	return &e, nil
}

// Set caches e for the TTL.
func (c *RedisEntitlementCache) Set(ctx context.Context, e *domain.Entitlements) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, CacheKey(e.UserID), raw, c.ttl).Err()
}

// Delete drops the owner's entry.
func (c *RedisEntitlementCache) Delete(ctx context.Context, userID string) error {
	return c.client.Del(ctx, CacheKey(userID)).Err()
}

// MemoryEntitlementCache is a per-process cache for local development and
// single-instance deployments without Redis.
type MemoryEntitlementCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]memoryEntry
}

type memoryEntry struct {
	value   domain.Entitlements
	expires time.Time
}

// NewMemoryEntitlementCache returns an empty in-memory cache.
func NewMemoryEntitlementCache(ttl time.Duration) *MemoryEntitlementCache {
	return &MemoryEntitlementCache{ttl: ttl, now: time.Now, entries: map[string]memoryEntry{}}
}

// Get returns a copy of the cached entitlements, or (nil, nil) on a miss.
func (c *MemoryEntitlementCache) Get(_ context.Context, userID string) (*domain.Entitlements, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[userID]
	if !ok || !c.now().Before(entry.expires) {
		return nil, nil //nolint:nilnil // cache miss
	}

	value := entry.value

	return &value, nil
}

// Set caches a copy of e.
func (c *MemoryEntitlementCache) Set(_ context.Context, e *domain.Entitlements) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[e.UserID] = memoryEntry{value: *e, expires: c.now().Add(c.ttl)}

	return nil
}

// Delete drops the owner's entry.
func (c *MemoryEntitlementCache) Delete(_ context.Context, userID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, userID)

	return nil
}
