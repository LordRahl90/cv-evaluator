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

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type payload struct {
	N    int    `json:"n"`
	Name string `json:"name"`
}

func newQueue(t *testing.T, clock *fakeClock, opts queue.Options) *queue.Queue {
	t.Helper()
	db := resetDB(t)
	if clock != nil {
		opts.Now = clock.Now
	}
	return queue.New(db, opts)
}

func TestEnqueueClaimComplete(t *testing.T) {
	q := newQueue(t, newFakeClock(), queue.Options{})
	ctx := t.Context()

	task, inserted, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{N: 7, Name: "x"})
	require.NoError(t, err)
	assert.True(t, inserted)
	assert.Equal(t, models.TaskPending, task.State)
	assert.Equal(t, queue.DefaultMaxAttempts, task.MaxAttempts)

	claimed, err := q.Claim(ctx, "w1")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, task.ID, claimed.ID)
	assert.Equal(t, models.TaskRunning, claimed.State)
	assert.Equal(t, 1, claimed.Attempts)
	assert.Equal(t, "w1", claimed.LockedBy)

	var got payload
	require.NoError(t, queue.Decode(claimed, &got))
	assert.Equal(t, payload{N: 7, Name: "x"}, got)

	none, err := q.Claim(ctx, "w2")
	require.NoError(t, err)
	assert.Nil(t, none, "a leased task is not handed out twice")

	require.NoError(t, q.Complete(ctx, claimed))
	stored, err := q.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TaskCompleted, stored.State)
	assert.NotNil(t, stored.FinishedAt)
	assert.Nil(t, stored.LockedUntil)

	assert.ErrorIs(t, q.Complete(ctx, claimed), queue.ErrLeaseLost, "completing twice is rejected")
}

func TestClaim_Empty(t *testing.T) {
	q := newQueue(t, nil, queue.Options{})
	task, err := q.Claim(t.Context(), "w1")
	require.NoError(t, err)
	assert.Nil(t, task)
}

func TestClaim_FiltersByKindAndRunAt(t *testing.T) {
	clock := newFakeClock()
	q := newQueue(t, clock, queue.Options{})
	ctx := t.Context()

	_, _, err := q.Enqueue(ctx, queue.KindRunSearch, payload{N: 1})
	require.NoError(t, err)
	later, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{N: 2}, queue.WithRunAt(clock.Now().Add(time.Minute)))
	require.NoError(t, err)

	got, err := q.Claim(ctx, "w", queue.KindEmbedJob)
	require.NoError(t, err)
	assert.Nil(t, got, "a task scheduled in the future is not due")

	got, err = q.Claim(ctx, "w", queue.KindEvaluateJob, queue.KindProcessCV)
	require.NoError(t, err)
	assert.Nil(t, got, "other kinds are not claimed")

	clock.Advance(time.Minute)
	got, err = q.Claim(ctx, "w", queue.KindEmbedJob)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, later.ID, got.ID)
}

func TestClaim_OrdersByRunAt(t *testing.T) {
	clock := newFakeClock()
	q := newQueue(t, clock, queue.Options{})
	ctx := t.Context()

	second, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{N: 2}, queue.WithRunAt(clock.Now().Add(-time.Second)))
	require.NoError(t, err)
	first, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{N: 1}, queue.WithRunAt(clock.Now().Add(-time.Minute)))
	require.NoError(t, err)

	a, err := q.Claim(ctx, "w")
	require.NoError(t, err)
	b, err := q.Claim(ctx, "w")
	require.NoError(t, err)
	assert.Equal(t, first.ID, a.ID)
	assert.Equal(t, second.ID, b.ID)
}

func TestEnqueue_UniqueKeyDedupes(t *testing.T) {
	q := newQueue(t, newFakeClock(), queue.Options{})
	ctx := t.Context()

	first, inserted, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{N: 1}, queue.WithUniqueKey("embed:1"))
	require.NoError(t, err)
	require.True(t, inserted)

	dup, inserted, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{N: 2}, queue.WithUniqueKey("embed:1"))
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.Equal(t, first.ID, dup.ID)

	// Still deduped while running.
	claimed, err := q.Claim(ctx, "w")
	require.NoError(t, err)
	_, inserted, err = q.Enqueue(ctx, queue.KindEmbedJob, payload{N: 3}, queue.WithUniqueKey("embed:1"))
	require.NoError(t, err)
	assert.False(t, inserted)

	// A finished task frees the key.
	require.NoError(t, q.Complete(ctx, claimed))
	again, inserted, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{N: 4}, queue.WithUniqueKey("embed:1"))
	require.NoError(t, err)
	assert.True(t, inserted)
	assert.NotEqual(t, first.ID, again.ID)

	// Tasks without a key are never deduped.
	_, ins1, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{})
	require.NoError(t, err)
	_, ins2, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{})
	require.NoError(t, err)
	assert.True(t, ins1)
	assert.True(t, ins2)

	var active int64
	require.NoError(t, testDB.Model(&models.QueueTask{}).
		Where("unique_key = ? AND state IN ?", "embed:1", []string{models.TaskPending, models.TaskRunning}).
		Count(&active).Error)
	assert.Equal(t, int64(1), active)
}

func TestEnqueue_UniqueKeyConcurrent(t *testing.T) {
	q := newQueue(t, nil, queue.Options{})

	var (
		wg       sync.WaitGroup
		inserted atomic.Int32
	)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := q.Enqueue(t.Context(), queue.KindRunSearch, payload{}, queue.WithUniqueKey("search:abc"))
			assert.NoError(t, err)
			if ok {
				inserted.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), inserted.Load())
}

func TestFail_RetriesWithExponentialBackoff(t *testing.T) {
	clock := newFakeClock()
	q := newQueue(t, clock, queue.Options{BaseBackoff: 10 * time.Second, MaxBackoff: 25 * time.Second})
	ctx := t.Context()

	task, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{}, queue.WithMaxAttempts(4))
	require.NoError(t, err)

	wantDelays := []time.Duration{10 * time.Second, 20 * time.Second, 25 * time.Second}
	for i, delay := range wantDelays {
		claimed, err := q.Claim(ctx, "w")
		require.NoError(t, err)
		require.NotNil(t, claimed, "attempt %d", i+1)
		assert.Equal(t, i+1, claimed.Attempts)

		require.NoError(t, q.Fail(ctx, claimed, errors.New("boom")))
		stored, err := q.Get(ctx, task.ID)
		require.NoError(t, err)
		assert.Equal(t, models.TaskPending, stored.State)
		assert.Equal(t, "boom", stored.LastError)
		assert.WithinDuration(t, clock.Now().Add(delay), stored.RunAt, time.Millisecond)

		// Not claimable before the backoff elapses.
		early, err := q.Claim(ctx, "w")
		require.NoError(t, err)
		assert.Nil(t, early)
		clock.Advance(delay)
	}

	// Fourth and last attempt fails: the task is dead.
	claimed, err := q.Claim(ctx, "w")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, 4, claimed.Attempts)
	require.NoError(t, q.Fail(ctx, claimed, errors.New("final")))

	stored, err := q.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TaskDead, stored.State)
	assert.Equal(t, "final", stored.LastError)
	assert.NotNil(t, stored.FinishedAt)

	clock.Advance(time.Hour)
	none, err := q.Claim(ctx, "w")
	require.NoError(t, err)
	assert.Nil(t, none, "dead tasks are never claimed")
}

func TestBackoff(t *testing.T) {
	q := queue.New(testDB, queue.Options{BaseBackoff: time.Second, MaxBackoff: time.Minute})
	assert.Equal(t, time.Second, q.Backoff(0))
	assert.Equal(t, time.Second, q.Backoff(1))
	assert.Equal(t, 2*time.Second, q.Backoff(2))
	assert.Equal(t, 32*time.Second, q.Backoff(6))
	assert.Equal(t, time.Minute, q.Backoff(7))
	assert.Equal(t, time.Minute, q.Backoff(1000))
}

func TestFail_PermanentGoesDead(t *testing.T) {
	q := newQueue(t, newFakeClock(), queue.Options{})
	ctx := t.Context()

	task, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{})
	require.NoError(t, err)
	claimed, err := q.Claim(ctx, "w")
	require.NoError(t, err)

	require.NoError(t, q.Fail(ctx, claimed, queue.Permanent(errors.New("bad payload"))))
	stored, err := q.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TaskDead, stored.State)
	assert.Equal(t, 1, stored.Attempts)
}

func TestFail_RetryAfterOverridesBackoff(t *testing.T) {
	clock := newFakeClock()
	q := newQueue(t, clock, queue.Options{BaseBackoff: time.Second})
	ctx := t.Context()

	task, _, err := q.Enqueue(ctx, queue.KindRunSearch, payload{})
	require.NoError(t, err)
	claimed, err := q.Claim(ctx, "w")
	require.NoError(t, err)

	require.NoError(t, q.Fail(ctx, claimed, queue.RetryAfter(errors.New("429"), 5*time.Minute)))
	stored, err := q.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TaskPending, stored.State)
	assert.WithinDuration(t, clock.Now().Add(5*time.Minute), stored.RunAt, time.Millisecond)
}

func TestVisibilityTimeout_ReclaimsCrashedTask(t *testing.T) {
	clock := newFakeClock()
	q := newQueue(t, clock, queue.Options{VisibilityTimeout: time.Minute})
	ctx := t.Context()

	task, _, err := q.Enqueue(ctx, queue.KindEvaluateJob, payload{}, queue.WithUniqueKey("eval:1"))
	require.NoError(t, err)

	crashed, err := q.Claim(ctx, "crashed-worker")
	require.NoError(t, err)
	require.NotNil(t, crashed)

	clock.Advance(59 * time.Second)
	none, err := q.Claim(ctx, "w2")
	require.NoError(t, err)
	assert.Nil(t, none, "lease still valid")

	clock.Advance(2 * time.Second)
	reclaimed, err := q.Claim(ctx, "w2")
	require.NoError(t, err)
	require.NotNil(t, reclaimed)
	assert.Equal(t, task.ID, reclaimed.ID)
	assert.Equal(t, 2, reclaimed.Attempts)
	assert.Equal(t, "w2", reclaimed.LockedBy)

	// The crashed worker comes back: it no longer owns the task.
	assert.ErrorIs(t, q.Complete(ctx, crashed), queue.ErrLeaseLost)
	assert.ErrorIs(t, q.Fail(ctx, crashed, errors.New("late")), queue.ErrLeaseLost)
	assert.ErrorIs(t, q.Extend(ctx, crashed), queue.ErrLeaseLost)

	require.NoError(t, q.Complete(ctx, reclaimed))
}

func TestVisibilityTimeout_LastAttemptGoesDead(t *testing.T) {
	clock := newFakeClock()
	q := newQueue(t, clock, queue.Options{VisibilityTimeout: time.Minute})
	ctx := t.Context()

	task, _, err := q.Enqueue(ctx, queue.KindEvaluateJob, payload{}, queue.WithMaxAttempts(1), queue.WithUniqueKey("k"))
	require.NoError(t, err)
	_, err = q.Claim(ctx, "crashed")
	require.NoError(t, err)

	clock.Advance(2 * time.Minute)
	got, err := q.Claim(ctx, "w2")
	require.NoError(t, err)
	assert.Nil(t, got)

	stored, err := q.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TaskDead, stored.State)
	assert.Contains(t, stored.LastError, "expired")

	// The dead task frees its unique key.
	_, inserted, err := q.Enqueue(ctx, queue.KindEvaluateJob, payload{}, queue.WithUniqueKey("k"))
	require.NoError(t, err)
	assert.True(t, inserted)
}

func TestExtend_KeepsLease(t *testing.T) {
	clock := newFakeClock()
	q := newQueue(t, clock, queue.Options{VisibilityTimeout: time.Minute})
	ctx := t.Context()

	_, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{})
	require.NoError(t, err)
	claimed, err := q.Claim(ctx, "w1")
	require.NoError(t, err)

	clock.Advance(50 * time.Second)
	require.NoError(t, q.Extend(ctx, claimed))
	clock.Advance(50 * time.Second)

	none, err := q.Claim(ctx, "w2")
	require.NoError(t, err)
	assert.Nil(t, none, "an extended lease is still held")
	require.NoError(t, q.Complete(ctx, claimed))
}

func TestClaim_ConcurrentClaimersNeverShareATask(t *testing.T) {
	q := newQueue(t, nil, queue.Options{})
	ctx := t.Context()

	const tasks = 200
	for i := range tasks {
		_, _, err := q.Enqueue(ctx, queue.KindEmbedJob, payload{N: i})
		require.NoError(t, err)
	}

	var (
		mu      sync.Mutex
		claimed = map[ulid.ULID]string{}
		dupes   []ulid.ULID
		wg      sync.WaitGroup
	)
	for w := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := string(rune('a' + w))
			for {
				task, err := q.Claim(context.Background(), worker)
				if !assert.NoError(t, err) || task == nil {
					return
				}
				mu.Lock()
				if _, seen := claimed[task.ID]; seen {
					dupes = append(dupes, task.ID)
				}
				claimed[task.ID] = worker
				mu.Unlock()
				assert.NoError(t, q.Complete(context.Background(), task))
			}
		}()
	}
	wg.Wait()

	assert.Empty(t, dupes, "no task may be claimed twice")
	assert.Len(t, claimed, tasks)

	var completed int64
	require.NoError(t, testDB.Model(&models.QueueTask{}).Where("state = ?", models.TaskCompleted).Count(&completed).Error)
	assert.Equal(t, int64(tasks), completed)
}

func TestEnqueue_RejectsUnmarshalablePayload(t *testing.T) {
	q := newQueue(t, nil, queue.Options{})
	_, _, err := q.Enqueue(t.Context(), queue.KindEmbedJob, make(chan int))
	require.Error(t, err)
}

func TestDecode_BadPayloadIsPermanent(t *testing.T) {
	err := queue.Decode(&queue.Task{Kind: "x", Payload: "not json"}, &payload{})
	require.Error(t, err)

	q := newQueue(t, newFakeClock(), queue.Options{})
	ctx := t.Context()
	task, _, err2 := q.Enqueue(ctx, queue.KindEmbedJob, payload{})
	require.NoError(t, err2)
	claimed, err2 := q.Claim(ctx, "w")
	require.NoError(t, err2)
	require.NoError(t, q.Fail(ctx, claimed, err))
	stored, err2 := q.Get(ctx, task.ID)
	require.NoError(t, err2)
	assert.Equal(t, models.TaskDead, stored.State)
}
