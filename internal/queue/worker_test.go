package queue_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/queue"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 10*time.Second, 20*time.Millisecond)
}

func countState(t *testing.T, state string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Model(&models.QueueTask{}).Where("state = ?", state).Count(&n).Error)
	return n
}

func TestWorker_ProcessesAndRetries(t *testing.T) {
	q := newQueue(t, nil, queue.Options{BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond})
	ctx := t.Context()

	for i := range 20 {
		_, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{N: i})
		require.NoError(t, err)
	}
	_, _, err := q.Enqueue(ctx, queue.KindEvaluateJob, payload{N: -1}, queue.WithMaxAttempts(3))
	require.NoError(t, err)
	// A kind without a handler is left alone.
	_, _, err = q.Enqueue(ctx, queue.KindProcessCV, payload{})
	require.NoError(t, err)

	var (
		mu       sync.Mutex
		seen     = map[int]int{}
		failures atomic.Int32
	)
	w := queue.NewWorker(q, map[queue.Kind]queue.Handler{
		queue.KindEmbedJob: func(_ context.Context, task *queue.Task) error {
			var p payload
			if err := queue.Decode(task, &p); err != nil {
				return err
			}
			mu.Lock()
			seen[p.N]++
			mu.Unlock()
			return nil
		},
		queue.KindEvaluateJob: func(context.Context, *queue.Task) error {
			failures.Add(1)
			return errors.New("always fails")
		},
	}, queue.WorkerOptions{Concurrency: 4, PollInterval: 10 * time.Millisecond})

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		w.Run(runCtx)
		close(done)
	}()

	waitFor(t, func() bool {
		return countState(t, models.TaskCompleted) == 20 && countState(t, models.TaskDead) == 1
	})
	cancel()
	<-done

	assert.Equal(t, int32(3), failures.Load(), "retried up to MaxAttempts")
	assert.Len(t, seen, 20)
	for n, c := range seen {
		assert.Equal(t, 1, c, "task %d ran once", n)
	}
	assert.Equal(t, int64(1), countState(t, models.TaskPending), "process_cv has no handler and stays pending")
}

func TestWorker_RecoversPanics(t *testing.T) {
	q := newQueue(t, nil, queue.Options{})
	ctx := t.Context()

	task, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{}, queue.WithMaxAttempts(1))
	require.NoError(t, err)

	w := queue.NewWorker(q, map[queue.Kind]queue.Handler{
		queue.KindEmbedJob: func(context.Context, *queue.Task) error { panic("kaboom") },
	}, queue.WorkerOptions{})

	worked, err := w.RunOnce(ctx)
	require.NoError(t, err)
	assert.True(t, worked)

	stored, err := q.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TaskDead, stored.State)
	assert.Contains(t, stored.LastError, "kaboom")
}

func TestWorker_HeartbeatExtendsLease(t *testing.T) {
	q := newQueue(t, nil, queue.Options{VisibilityTimeout: 300 * time.Millisecond})
	ctx := t.Context()

	_, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{})
	require.NoError(t, err)

	release := make(chan struct{})
	started := make(chan struct{})
	w := queue.NewWorker(q, map[queue.Kind]queue.Handler{
		queue.KindEmbedJob: func(ctx context.Context, _ *queue.Task) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}, queue.WorkerOptions{})

	result := make(chan error, 1)
	go func() {
		_, err := w.RunOnce(ctx)
		result <- err
	}()
	<-started

	// Well past the visibility timeout, the lease is still held.
	time.Sleep(time.Second)
	stolen, err := q.Claim(ctx, "thief")
	require.NoError(t, err)
	assert.Nil(t, stolen)

	close(release)
	require.NoError(t, <-result)
	assert.Equal(t, int64(1), countState(t, models.TaskCompleted))
}

func TestWorker_GracefulShutdownLetsInFlightFinish(t *testing.T) {
	q := newQueue(t, nil, queue.Options{})
	ctx := t.Context()

	_, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{})
	require.NoError(t, err)

	started := make(chan struct{})
	w := queue.NewWorker(q, map[queue.Kind]queue.Handler{
		queue.KindEmbedJob: func(ctx context.Context, _ *queue.Task) error {
			close(started)
			select {
			case <-time.After(200 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}, queue.WorkerOptions{PollInterval: 10 * time.Millisecond, ShutdownGrace: 5 * time.Second})

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		w.Run(runCtx)
		close(done)
	}()
	<-started
	cancel()
	<-done

	assert.Equal(t, int64(1), countState(t, models.TaskCompleted), "in-flight task finished after shutdown began")
}

func TestWorker_ShutdownGraceCancelsSlowTasks(t *testing.T) {
	q := newQueue(t, nil, queue.Options{})
	ctx := t.Context()

	task, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{})
	require.NoError(t, err)

	started := make(chan struct{})
	w := queue.NewWorker(q, map[queue.Kind]queue.Handler{
		queue.KindEmbedJob: func(ctx context.Context, _ *queue.Task) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	}, queue.WorkerOptions{PollInterval: 10 * time.Millisecond, ShutdownGrace: 50 * time.Millisecond})

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		w.Run(runCtx)
		close(done)
	}()
	<-started
	cancel()
	<-done

	stored, err := q.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TaskPending, stored.State, "a cancelled task goes back to be retried")
	assert.Contains(t, stored.LastError, "context canceled")
}
