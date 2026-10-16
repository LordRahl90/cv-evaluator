package pipeline

import (
	"context"
	"errors"
	"sync"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
)

// RateLimitOptions configures per-portal rate limiting.
type RateLimitOptions struct {
	// Interval is the minimum time between two calls to the same portal.
	// Default 2s.
	Interval time.Duration
	// PerProvider overrides Interval for named portals.
	PerProvider map[string]time.Duration
	// MaxPenalty caps the extra delay added after consecutive failures.
	// Each failure doubles the penalty, starting at the interval; a success
	// clears it. Default 10m.
	MaxPenalty time.Duration
}

func (o RateLimitOptions) withDefaults() RateLimitOptions {
	if o.Interval <= 0 {
		o.Interval = 2 * time.Second
	}
	if o.MaxPenalty <= 0 {
		o.MaxPenalty = 10 * time.Minute
	}
	return o
}

type providerState struct {
	next    time.Time
	penalty time.Duration
}

// RateLimiter spaces out calls per portal and backs off after failures. It
// is safe for concurrent use; limits apply within one process.
type RateLimiter struct {
	opts  RateLimitOptions
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	mu     sync.Mutex
	states map[string]*providerState
}

// NewRateLimiter returns a limiter with the given options.
func NewRateLimiter(opts RateLimitOptions) *RateLimiter {
	return &RateLimiter{
		opts:   opts.withDefaults(),
		now:    time.Now,
		sleep:  sleepCtx,
		states: map[string]*providerState{},
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (l *RateLimiter) interval(provider string) time.Duration {
	if d, ok := l.opts.PerProvider[provider]; ok {
		return d
	}
	return l.opts.Interval
}

func (l *RateLimiter) state(provider string) *providerState {
	st, ok := l.states[provider]
	if !ok {
		st = &providerState{}
		l.states[provider] = st
	}
	return st
}

// reserve books the next slot for provider and returns how long the caller
// must wait for it.
func (l *RateLimiter) reserve(provider string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	st := l.state(provider)
	start := now
	if st.next.After(now) {
		start = st.next
	}
	st.next = start.Add(l.interval(provider))
	return start.Sub(now)
}

// Wait blocks until provider may be called, or ctx is done.
func (l *RateLimiter) Wait(ctx context.Context, provider string) error {
	ctx, span := telemetry.Start(ctx, "pipeline.RateLimiter.Wait", attribute.String(telemetry.AttrProvider, provider))
	defer span.End()
	delay := l.reserve(provider)
	span.SetAttributes(attribute.Int64("wait_ms", delay.Milliseconds()))
	return l.sleep(ctx, delay)
}

// Report records the outcome of a call. A failure doubles the portal's
// penalty (capped at MaxPenalty) and pushes its next slot out by it; a
// success clears the penalty.
func (l *RateLimiter) Report(provider string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	st := l.state(provider)
	if err == nil {
		st.penalty = 0
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	if st.penalty == 0 {
		st.penalty = l.interval(provider)
	} else {
		st.penalty *= 2
	}
	if st.penalty > l.opts.MaxPenalty {
		st.penalty = l.opts.MaxPenalty
	}
	if next := l.now().Add(st.penalty); next.After(st.next) {
		st.next = next
	}
}

// limitedFetcher applies a RateLimiter to every portal call.
type limitedFetcher struct {
	Fetcher
	limiter *RateLimiter
}

func (f *limitedFetcher) Search(ctx context.Context, provider string, c Criteria) ([]entities.JobPost, error) {
	if err := f.limiter.Wait(ctx, provider); err != nil {
		return nil, err
	}
	posts, err := f.Fetcher.Search(ctx, provider, c)
	f.limiter.Report(provider, err)
	return posts, err
}

func (f *limitedFetcher) Details(ctx context.Context, provider string, job *entities.JobPost) (*entities.JobDetails, error) {
	if err := f.limiter.Wait(ctx, provider); err != nil {
		return nil, err
	}
	details, err := f.Fetcher.Details(ctx, provider, job)
	f.limiter.Report(provider, err)
	return details, err
}
