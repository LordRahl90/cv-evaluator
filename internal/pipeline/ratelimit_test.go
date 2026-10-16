package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLimiterClock records sleeps and advances time by them.
type fakeLimiterClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeLimiterClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeLimiterClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func newTestLimiter(opts RateLimitOptions) (*RateLimiter, *fakeLimiterClock) {
	clock := &fakeLimiterClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	l := NewRateLimiter(opts)
	l.now = clock.Now
	l.sleep = clock.Sleep
	return l, clock
}

func TestRateLimiter_SpacesCallsPerProvider(t *testing.T) {
	l, clock := newTestLimiter(RateLimitOptions{
		Interval:    time.Second,
		PerProvider: map[string]time.Duration{"jobindex": 3 * time.Second},
	})
	ctx := t.Context()

	require.NoError(t, l.Wait(ctx, "linkedin"))
	require.NoError(t, l.Wait(ctx, "linkedin"))
	require.NoError(t, l.Wait(ctx, "linkedin"))
	assert.Equal(t, []time.Duration{0, time.Second, time.Second}, clock.sleeps)

	// Another portal has its own budget.
	clock.sleeps = nil
	require.NoError(t, l.Wait(ctx, "jobindex"))
	require.NoError(t, l.Wait(ctx, "jobindex"))
	assert.Equal(t, []time.Duration{0, 3 * time.Second}, clock.sleeps)
}

func TestRateLimiter_BacksOffAfterFailures(t *testing.T) {
	l, clock := newTestLimiter(RateLimitOptions{Interval: time.Second, MaxPenalty: 5 * time.Second})
	ctx := t.Context()
	boom := errors.New("429")

	require.NoError(t, l.Wait(ctx, "linkedin"))
	l.Report("linkedin", boom) // penalty 1s
	require.NoError(t, l.Wait(ctx, "linkedin"))
	l.Report("linkedin", boom) // penalty 2s
	require.NoError(t, l.Wait(ctx, "linkedin"))
	l.Report("linkedin", boom) // penalty 4s
	require.NoError(t, l.Wait(ctx, "linkedin"))
	l.Report("linkedin", boom) // penalty 5s (capped)
	require.NoError(t, l.Wait(ctx, "linkedin"))
	assert.Equal(t, []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second}, clock.sleeps)

	// A success clears the penalty.
	l.Report("linkedin", nil)
	clock.sleeps = nil
	require.NoError(t, l.Wait(ctx, "linkedin"))
	assert.Equal(t, []time.Duration{time.Second}, clock.sleeps)
}

func TestRateLimiter_PenaltyIsPerProvider(t *testing.T) {
	l, clock := newTestLimiter(RateLimitOptions{Interval: time.Second, MaxPenalty: time.Hour})
	ctx := t.Context()

	require.NoError(t, l.Wait(ctx, "linkedin"))
	for range 6 {
		l.Report("linkedin", errors.New("blocked"))
	}
	clock.sleeps = nil
	require.NoError(t, l.Wait(ctx, "jobindex"))
	assert.Equal(t, []time.Duration{0}, clock.sleeps, "a failing portal does not slow the others")
}

func TestRateLimiter_HonoursContext(t *testing.T) {
	l := NewRateLimiter(RateLimitOptions{Interval: time.Hour})
	require.NoError(t, l.Wait(t.Context(), "linkedin"))

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, l.Wait(ctx, "linkedin"), context.DeadlineExceeded)
}
