// Package ratelimit limits how often a key (such as a user id) may do
// something within a sliding time window.
//
// The limiter is in memory: limits apply per API process and reset when it
// restarts. That is enough while the API runs as a single instance; running
// several instances multiplies the effective limit by the number of
// instances, and a shared store (for example Postgres or Redis) would be
// needed to enforce one limit across them.
package ratelimit

import (
	"sync"
	"time"
)

// sweepEvery is how many calls to Allow pass between sweeps that forget
// keys with no recent hits, so the limiter's memory stays bounded by the
// number of recently active keys.
const sweepEvery = 1024

// Limiter allows at most Limit events per key in any Window. It is safe for
// concurrent use.
type Limiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

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

// Limit and Window return the configured limit.
func (l *Limiter) Limit() int { return l.limit }

// Window is the time window the limit applies to.
func (l *Limiter) Window() time.Duration { return l.window }

// Allow records an event for key and reports whether it is within the
// limit. When it is not, nothing is recorded and retryAfter says when the
// next event will be allowed.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
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
