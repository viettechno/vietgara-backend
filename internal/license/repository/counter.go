package repository

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// counterTTL keeps a day's counter a little longer than the day.
const counterTTL = 48 * time.Hour

func counterKey(userID, day string) string {
	return "license:transactions:" + userID + ":" + day
}

// RedisTransactionCounter counts business documents per owner and day in
// Redis, shared by every backend instance.
type RedisTransactionCounter struct {
	client redis.UniversalClient
}

// NewRedisTransactionCounter returns a counter over client.
func NewRedisTransactionCounter(client redis.UniversalClient) *RedisTransactionCounter {
	return &RedisTransactionCounter{client: client}
}

// Increment adds one and returns the day's count.
func (c *RedisTransactionCounter) Increment(ctx context.Context, userID, day string) (int, error) {
	key := counterKey(userID, day)

	pipe := c.client.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, counterTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}

	return int(incr.Val()), nil
}

// Get returns the day's count.
func (c *RedisTransactionCounter) Get(ctx context.Context, userID, day string) (int, error) {
	n, err := c.client.Get(ctx, counterKey(userID, day)).Int()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}

	return n, err
}

// MemoryTransactionCounter is the per-process counter used without Redis.
type MemoryTransactionCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

// NewMemoryTransactionCounter returns an empty counter.
func NewMemoryTransactionCounter() *MemoryTransactionCounter {
	return &MemoryTransactionCounter{counts: map[string]int{}}
}

// Increment adds one and returns the day's count.
func (c *MemoryTransactionCounter) Increment(_ context.Context, userID, day string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.counts[counterKey(userID, day)]++

	return c.counts[counterKey(userID, day)], nil
}

// Get returns the day's count.
func (c *MemoryTransactionCounter) Get(_ context.Context, userID, day string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.counts[counterKey(userID, day)], nil
}
