package pipeline

import (
	"context"
	"time"
)

// Observer kinds: one per pipeline step.
const (
	KindRunSearch    = "run_search"    // one search on one portal
	KindEmbedJob     = "embed_job"     // fetch a job's details if needed and embed it
	KindEvaluateJob  = "evaluate_job"  // evaluate one search result
	KindScheduleTick = "schedule_tick" // one scheduler pass over due searches
)

// RunObserver is told about every background run: each step (kind
// run_search per portal, embed_job, evaluate_job) and each scheduler tick
// (kind schedule_tick). It is the hook for metrics and tracing.
//
// RunStarted may return a derived context (e.g. carrying a span); the run
// uses it, so anything the run does nests under it. RunFinished receives
// that context, the run's duration and its error (nil on success).
type RunObserver interface {
	RunStarted(ctx context.Context, kind string) context.Context
	RunFinished(ctx context.Context, kind string, d time.Duration, err error)
}

// NopObserver is the default RunObserver; it does nothing.
type NopObserver struct{}

func (NopObserver) RunStarted(ctx context.Context, _ string) context.Context { return ctx }

func (NopObserver) RunFinished(context.Context, string, time.Duration, error) {}

func observerOrNop(o RunObserver) RunObserver {
	if o == nil {
		return NopObserver{}
	}
	return o
}

// observe runs fn between RunStarted and RunFinished.
func observe(ctx context.Context, o RunObserver, kind string, fn func(context.Context) error) error {
	ctx = o.RunStarted(ctx, kind)
	start := time.Now()
	err := fn(ctx)
	o.RunFinished(ctx, kind, time.Since(start), err)
	return err
}
