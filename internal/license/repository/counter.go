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

// tryIncrementScript adds one unless the count has reached the limit, and
// refreshes the key's lifetime. It returns 1 when it counted and 0 when not.
var tryIncrementScript = redis.NewScript(`
local n = tonumber(redis.call('GET', KEYS[1])) or 0
if n >= tonumber(ARGV[1]) then
  return 0
end
redis.call('INCR', KEYS[1])
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`)

// TryIncrement adds one to the day's count unless it is already at limit.
func (c *RedisTransactionCounter) TryIncrement(ctx context.Context, userID, day string, limit int) (bool, error) {
	n, err := tryIncrementScript.Run(ctx, c.client, []string{counterKey(userID, day)}, limit, counterTTL.Milliseconds()).Int()
	if err != nil {
		return false, err
	}

	return n == 1, nil
}

// decrementScript lowers the count by one only while it is above zero, so a
// key that expired or was lost is not recreated without a lifetime.
var decrementScript = redis.NewScript(`
local n = tonumber(redis.call('GET', KEYS[1]))
if n and n > 0 then
  return redis.call('DECR', KEYS[1])
end
return 0
`)

// Decrement takes one back from the day's count, never below zero.
func (c *RedisTransactionCounter) Decrement(ctx context.Context, userID, day string) error {
	return decrementScript.Run(ctx, c.client, []string{counterKey(userID, day)}).Err()
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

// TryIncrement adds one to the day's count unless it is already at limit.
func (c *MemoryTransactionCounter) TryIncrement(_ context.Context, userID, day string, limit int) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := counterKey(userID, day)
	if c.counts[key] >= limit {
		return false, nil
	}

	c.counts[key]++

	return true, nil
}

// Decrement takes one back from the day's count, never below zero.
func (c *MemoryTransactionCounter) Decrement(_ context.Context, userID, day string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if key := counterKey(userID, day); c.counts[key] > 0 {
		c.counts[key]--
	}

	return nil
}

// Get returns the day's count.
func (c *MemoryTransactionCounter) Get(_ context.Context, userID, day string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.counts[counterKey(userID, day)], nil
}
