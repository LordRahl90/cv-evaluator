// Package ratelimit limits how often a key (such as a user id or client IP)
// may do something within a time window.
//
// New keeps a sliding window in memory: limits apply per API process and
// reset when it restarts. That suits development and tests. NewShared keeps
// a fixed window in a Store such as Redis or Valkey, so one limit holds
// across every API replica and survives restarts.
package ratelimit

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// sweepEvery is how many calls to Allow pass between sweeps that forget
// keys with no recent hits, so the limiter's memory stays bounded by the
// number of recently active keys.
const sweepEvery = 1024

// Store counts events in a shared, expiring counter.
type Store interface {
	// Incr adds one to key, creating it with a lifetime of window when it
	// does not exist, and returns the new count and the time left.
	Incr(ctx context.Context, key string, window time.Duration) (count int64, ttl time.Duration, err error)
}

// Limiter allows at most Limit events per key in a Window. It is safe for
// concurrent use.
type Limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	// store and name are set for a shared limiter; otherwise hits are
	// kept in memory below.
	store Store
	name  string

	mu    sync.Mutex
	hits  map[string][]time.Time
	calls int
}

// New returns a limiter that allows limit events per key in any window.
func New(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:  max(1, limit),
		window: window,
		now:    time.Now,
		hits:   map[string][]time.Time{},
	}
}

// NewShared returns a limiter that keeps its counts in store, under keys
// prefixed with name. The window starts at a key's first event. When store
// fails, events are allowed and the error is logged: an outage of the
// store must not lock everyone out.
func NewShared(store Store, name string, limit int, window time.Duration) *Limiter {
	l := New(limit, window)
	l.store = store
	l.name = name
	return l
}

// Limit and Window return the configured limit.
func (l *Limiter) Limit() int { return l.limit }

// Window is the time window the limit applies to.
func (l *Limiter) Window() time.Duration { return l.window }

// Allow is AllowContext with a background context.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	return l.AllowContext(context.Background(), key)
}

// AllowContext records an event for key and reports whether it is within
// the limit. When it is not, retryAfter says when the next event will be
// allowed.
func (l *Limiter) AllowContext(ctx context.Context, key string) (ok bool, retryAfter time.Duration) {
	if l.store != nil {
		return l.allowShared(ctx, key)
	}
	return l.allowLocal(key)
}

func (l *Limiter) allowShared(ctx context.Context, key string) (bool, time.Duration) {
	count, ttl, err := l.store.Incr(ctx, "ratelimit:"+l.name+":"+key, l.window)
	if err != nil {
		slog.ErrorContext(ctx, "rate limit store failed; allowing the request", "limiter", l.name, "error", err)
		return true, 0
	}
	if count > int64(l.limit) {
		if ttl <= 0 {
			ttl = l.window
		}
		return false, ttl
	}
	return true, 0
}

// allowLocal applies a sliding window in memory. A rejected event is not
// recorded.
func (l *Limiter) allowLocal(key string) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.calls++
	if l.calls%sweepEvery == 0 {
		l.sweep(now)
	}

	recent := l.recent(key, now)
	if len(recent) >= l.limit {
		l.hits[key] = recent
		return false, recent[0].Add(l.window).Sub(now)
	}
	l.hits[key] = append(recent, now)
	return true, 0
}

// recent returns key's hits inside the window ending at now, oldest first.
func (l *Limiter) recent(key string, now time.Time) []time.Time {
	hits := l.hits[key]
	cutoff := now.Add(-l.window)
	i := 0
	for i < len(hits) && !hits[i].After(cutoff) {
		i++
	}
	return hits[i:]
}

func (l *Limiter) sweep(now time.Time) {
	for key := range l.hits {
		if len(l.recent(key, now)) == 0 {
			delete(l.hits, key)
		}
	}
}
