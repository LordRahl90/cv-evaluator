package pipeline

import (
	"context"
	"time"

	"cv-evaluator/internal/queue"
)

// KindScheduleTick is the observer kind for one scheduler tick.
const KindScheduleTick = "schedule_tick"

// RunObserver is told about every background run: each task handler
// execution (kind run_search, embed_job or evaluate_job) and each scheduler
// tick (kind schedule_tick). It is the hook for metrics and tracing.
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

// observed wraps a queue handler with the observer.
func observed(o RunObserver, kind queue.Kind, h queue.Handler) queue.Handler {
	return func(ctx context.Context, task *queue.Task) error {
		return observe(ctx, o, string(kind), func(ctx context.Context) error {
			return h(ctx, task)
		})
	}
}
