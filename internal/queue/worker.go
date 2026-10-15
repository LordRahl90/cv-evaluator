package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// Handler processes one task. Returning nil completes it; returning an
// error fails the attempt (see Queue.Fail, Permanent and RetryAfter).
// Handlers must be idempotent: a task can run more than once, e.g. when a
// worker crashes after doing the work but before completing the task.
type Handler func(ctx context.Context, task *Task) error

// WorkerOptions configures a Worker.
type WorkerOptions struct {
	// ID names the worker in task leases. Defaults to host-pid-random.
	ID string
	// Concurrency is the number of tasks processed at once. Default 1.
	Concurrency int
	// PollInterval is how long an idle worker waits before polling again.
	// Default 1s.
	PollInterval time.Duration
	// ShutdownGrace is how long in-flight tasks may keep running after the
	// worker is asked to stop. Their context is cancelled after it, so they
	// fail and get retried. Default 30s.
	ShutdownGrace time.Duration
}

// Worker claims tasks from a Queue and runs their handlers.
type Worker struct {
	queue    *Queue
	handlers map[Kind]Handler
	kinds    []Kind
	opts     WorkerOptions
}

// NewWorker returns a worker that only claims the kinds in handlers.
func NewWorker(q *Queue, handlers map[Kind]Handler, opts WorkerOptions) *Worker {
	if opts.ID == "" {
		host, _ := os.Hostname()
		opts.ID = fmt.Sprintf("%s-%d-%s", host, os.Getpid(), ulid.Make().String()[20:])
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.ShutdownGrace <= 0 {
		opts.ShutdownGrace = 30 * time.Second
	}

	kinds := make([]Kind, 0, len(handlers))
	for k := range handlers {
		kinds = append(kinds, k)
	}
	return &Worker{queue: q, handlers: handlers, kinds: kinds, opts: opts}
}

// ID returns the worker's lease name.
func (w *Worker) ID() string {
	return w.opts.ID
}

// Run processes tasks until ctx is cancelled, then waits for in-flight
// tasks (up to ShutdownGrace) and returns.
func (w *Worker) Run(ctx context.Context) {
	// Handlers run on a context that outlives ctx by ShutdownGrace.
	handlerCtx, cancelHandlers := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelHandlers()
	stopGrace := context.AfterFunc(ctx, func() {
		time.AfterFunc(w.opts.ShutdownGrace, cancelHandlers)
	})
	defer stopGrace()

	var wg sync.WaitGroup
	for range w.opts.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.loop(ctx, handlerCtx)
		}()
	}
	wg.Wait()
}

func (w *Worker) loop(ctx, handlerCtx context.Context) {
	for ctx.Err() == nil {
		worked, err := w.runOne(ctx, handlerCtx)
		if err != nil {
			slog.ErrorContext(ctx, "queue worker error", "worker", w.opts.ID, "error", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
		case <-time.After(w.opts.PollInterval):
		}
	}
}

// RunOnce claims and processes at most one task. It reports whether a task
// was processed. It is mainly useful in tests.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	return w.runOne(ctx, ctx)
}

func (w *Worker) runOne(claimCtx, handlerCtx context.Context) (bool, error) {
	task, err := w.queue.Claim(claimCtx, w.opts.ID, w.kinds...)
	if err != nil || task == nil {
		return false, err
	}

	log := slog.With("worker", w.opts.ID, "task_id", task.ID.String(), "kind", task.Kind, "attempt", task.Attempts)
	log.DebugContext(handlerCtx, "task claimed")

	start := time.Now()
	runErr := w.execute(handlerCtx, task)

	// Record the outcome even if the handler context was cancelled.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(handlerCtx), 10*time.Second)
	defer cancel()

	if runErr == nil {
		if err := w.queue.Complete(finishCtx, task); err != nil {
			return true, fmt.Errorf("complete task %s: %w", task.ID, err)
		}
		log.InfoContext(handlerCtx, "task completed", "duration", time.Since(start))
		return true, nil
	}

	if err := w.queue.Fail(finishCtx, task, runErr); err != nil {
		return true, fmt.Errorf("fail task %s (%v): %w", task.ID, runErr, err)
	}
	log.WarnContext(handlerCtx, "task failed", "error", runErr, "state", task.State,
		"next_run_at", task.RunAt, "duration", time.Since(start))
	return true, nil
}

// execute runs the handler while renewing the task's lease, and turns a
// panic into an error.
func (w *Worker) execute(ctx context.Context, task *Task) (err error) {
	handler, ok := w.handlers[Kind(task.Kind)]
	if !ok {
		return Permanent(fmt.Errorf("no handler for task kind %q", task.Kind))
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var hb sync.WaitGroup
	hb.Add(1)
	go func() {
		defer hb.Done()
		w.heartbeat(ctx, cancel, task)
	}()
	defer hb.Wait()
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v\n%s", r, debug.Stack())
		}
	}()
	return handler(ctx, task)
}

// heartbeat extends the lease every third of the visibility timeout. If the
// lease is lost the handler's context is cancelled.
func (w *Worker) heartbeat(ctx context.Context, cancel context.CancelFunc, task *Task) {
	interval := w.queue.VisibilityTimeout() / 3
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Work on a copy so the handler goroutine's task is never written here.
	lease := *task
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := w.queue.Extend(ctx, &lease)
			if errors.Is(err, ErrLeaseLost) {
				slog.WarnContext(ctx, "task lease lost; cancelling handler", "task_id", task.ID.String())
				cancel()
				return
			}
			if err != nil && ctx.Err() == nil {
				slog.WarnContext(ctx, "extend task lease", "task_id", task.ID.String(), "error", err)
			}
		}
	}
}
