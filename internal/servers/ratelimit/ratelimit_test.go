package ratelimit

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClock is a settable time source.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestLimiter(limit int, window time.Duration) (*Limiter, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	l := New(limit, window)
	l.now = clock.now
	return l, clock
}

func TestLimiter_SlidingWindow(t *testing.T) {
	l, clock := newTestLimiter(2, time.Minute)

	ok, _ := l.Allow("a")
	assert.True(t, ok)
	clock.advance(20 * time.Second)
	ok, _ = l.Allow("a")
	assert.True(t, ok)

	clock.advance(10 * time.Second)
	ok, retry := l.Allow("a")
	assert.False(t, ok)
	assert.Equal(t, 30*time.Second, retry, "the oldest hit leaves the window in 30s")

	ok, _ = l.Allow("b")
	assert.True(t, ok, "keys are limited separately")

	clock.advance(30 * time.Second)
	ok, _ = l.Allow("a")
	assert.True(t, ok, "the first hit has left the window")
	ok, retry = l.Allow("a")
	assert.False(t, ok)
	assert.Equal(t, 20*time.Second, retry)
}

func TestLimiter_RejectedCallsAreNotCounted(t *testing.T) {
	l, clock := newTestLimiter(1, time.Minute)
	ok, _ := l.Allow("a")
	require.True(t, ok)
	for range 5 {
		clock.advance(10 * time.Second)
		ok, _ = l.Allow("a")
		require.False(t, ok)
	}
	clock.advance(10 * time.Second)
	ok, _ = l.Allow("a")
	assert.True(t, ok, "retrying while limited does not extend the limit")
}

func TestLimiter_SweepsIdleKeys(t *testing.T) {
	l, clock := newTestLimiter(1, time.Minute)
	for i := range sweepEvery - 1 {
		l.Allow(strconv.Itoa(i))
	}
	clock.advance(2 * time.Minute)
	l.Allow("fresh")
	assert.Len(t, l.hits, 1)
}

func TestLimiter_Concurrent(t *testing.T) {
	l := New(10, time.Minute)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 50 {
		wg.Go(func() {
			if ok, _ := l.Allow("a"); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	assert.Equal(t, 10, allowed)
	assert.Equal(t, 10, l.Limit())
	assert.Equal(t, time.Minute, l.Window())
}
