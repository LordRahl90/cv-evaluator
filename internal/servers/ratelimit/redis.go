package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// incrScript adds one to a counter and gives a new counter its lifetime, in
// one atomic step, then returns the count and the milliseconds left.
var incrScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return {n, redis.call('PTTL', KEYS[1])}
`)

// RedisStore is a Store in Redis or Valkey.
type RedisStore struct {
	client redis.Scripter
}

// NewRedisStore returns a Store that keeps its counters in client.
func NewRedisStore(client redis.Scripter) *RedisStore {
	return &RedisStore{client: client}
}

// Incr implements Store.
func (s *RedisStore) Incr(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	res, err := incrScript.Run(ctx, s.client, []string{key}, window.Milliseconds()).Int64Slice()
	if err != nil {
		return 0, 0, err
	}
	if len(res) != 2 {
		return 0, 0, fmt.Errorf("rate limit script returned %d values", len(res))
	}
	return res[0], time.Duration(res[1]) * time.Millisecond, nil
}
