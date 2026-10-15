// Package queue is a small Postgres-backed job queue built on GORM.
//
// Tasks live in the queue_tasks table (models.QueueTask). Workers claim
// them with SELECT ... FOR UPDATE SKIP LOCKED, so concurrent workers, in
// one process or many, never get the same task. A claimed task carries a
// lease (LockedUntil); a worker that crashes stops renewing it, and once it
// expires another worker reclaims the task. Failed tasks are retried with
// exponential backoff until MaxAttempts, then marked dead.
//
// Every state change after a claim is fenced on (id, state = running,
// attempts), so a worker whose lease was taken over cannot complete or fail
// a task that now belongs to someone else.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cv-evaluator/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Kind names a type of task. Each kind has its own handler.
type Kind string

const (
	// KindRunSearch runs one saved job search and records its results.
	KindRunSearch Kind = "run_search"
	// KindEmbedJob fetches a job's details if needed and embeds it.
	KindEmbedJob Kind = "embed_job"
	// KindEvaluateJob evaluates a search result's job against the user's CV.
	KindEvaluateJob Kind = "evaluate_job"
	// KindProcessCV is reserved for CV processing (Phase 2). No handler
	// is registered for it yet, so tasks of this kind are never claimed.
	KindProcessCV Kind = "process_cv"
)

// Task is a queued unit of work.
type Task = models.QueueTask

// ErrLeaseLost is returned when a worker tries to finish a task it no
// longer holds: its lease expired and the task was reclaimed, or it was
// already finished.
var ErrLeaseLost = errors.New("queue: task lease lost")

// Defaults used when Options leaves a field zero.
const (
	DefaultVisibilityTimeout = 5 * time.Minute
	DefaultBaseBackoff       = 10 * time.Second
	DefaultMaxBackoff        = time.Hour
	DefaultMaxAttempts       = 5
)

// Options configures a Queue.
type Options struct {
	// VisibilityTimeout is how long a claim lasts before another worker may
	// take the task over. Workers renew it while a handler runs.
	VisibilityTimeout time.Duration
	// BaseBackoff is the delay before the first retry; each later retry
	// doubles it, up to MaxBackoff.
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	// MaxAttempts is the default number of attempts per task.
	MaxAttempts int
	// Now returns the current time. Tests use it to control the clock.
	Now func() time.Time
}

func (o Options) withDefaults() Options {
	if o.VisibilityTimeout <= 0 {
		o.VisibilityTimeout = DefaultVisibilityTimeout
	}
	if o.BaseBackoff <= 0 {
		o.BaseBackoff = DefaultBaseBackoff
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = DefaultMaxBackoff
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = DefaultMaxAttempts
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// Queue enqueues, claims and finishes tasks.
type Queue struct {
	db   *gorm.DB
	opts Options
}

// New returns a queue over db. The queue_tasks table must already exist
// (see internal/migrator).
func New(db *gorm.DB, opts Options) *Queue {
	return &Queue{db: db, opts: opts.withDefaults()}
}

// VisibilityTimeout returns the lease length.
func (q *Queue) VisibilityTimeout() time.Duration {
	return q.opts.VisibilityTimeout
}

func (q *Queue) now() time.Time {
	return q.opts.Now().UTC()
}

type enqueueOptions struct {
	uniqueKey   string
	runAt       time.Time
	maxAttempts int
}

// EnqueueOption customises one Enqueue call.
type EnqueueOption func(*enqueueOptions)

// WithUniqueKey dedupes the task: while a pending or running task with the
// same key exists, Enqueue returns it instead of adding another.
func WithUniqueKey(key string) EnqueueOption {
	return func(o *enqueueOptions) { o.uniqueKey = key }
}

// WithRunAt delays the task until t.
func WithRunAt(t time.Time) EnqueueOption {
	return func(o *enqueueOptions) { o.runAt = t }
}

// WithMaxAttempts overrides the queue's default attempt limit.
func WithMaxAttempts(n int) EnqueueOption {
	return func(o *enqueueOptions) { o.maxAttempts = n }
}

// activeKeyPredicate must match the partial unique index on unique_key in
// models.QueueTask so Postgres can infer it as the ON CONFLICT target.
const activeKeyPredicate = "state = 'pending' OR state = 'running'"

// Enqueue adds a task of kind with payload marshalled as JSON. It reports
// whether a new task was inserted; with WithUniqueKey and an active task
// already holding the key, it returns that task and false.
func (q *Queue) Enqueue(ctx context.Context, kind Kind, payload any, opts ...EnqueueOption) (*Task, bool, error) {
	o := enqueueOptions{maxAttempts: q.opts.MaxAttempts}
	for _, opt := range opts {
		opt(&o)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, false, fmt.Errorf("queue: marshal %s payload: %w", kind, err)
	}

	now := q.now()
	task := &Task{
		Kind:        string(kind),
		Payload:     string(body),
		State:       models.TaskPending,
		RunAt:       now,
		MaxAttempts: o.maxAttempts,
	}
	if !o.runAt.IsZero() {
		task.RunAt = o.runAt.UTC()
	}
	if o.uniqueKey != "" {
		key := o.uniqueKey
		task.UniqueKey = &key
	}

	if task.UniqueKey == nil {
		if err := q.db.WithContext(ctx).Create(task).Error; err != nil {
			return nil, false, fmt.Errorf("queue: enqueue %s: %w", kind, err)
		}
		return task, true, nil
	}

	// The holder of the key can finish between a conflicting insert and the
	// read of it, so retry a few times before giving up.
	for range 3 {
		res := q.db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:     []clause.Column{{Name: "unique_key"}},
				TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: activeKeyPredicate}}},
				DoNothing:   true,
			}).
			Create(task)
		if res.Error != nil {
			return nil, false, fmt.Errorf("queue: enqueue %s: %w", kind, res.Error)
		}
		if res.RowsAffected == 1 {
			return task, true, nil
		}

		var existing Task
		err := q.db.WithContext(ctx).
			Where("unique_key = ? AND ("+activeKeyPredicate+")", *task.UniqueKey).
			First(&existing).Error
		if err == nil {
			return &existing, false, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, fmt.Errorf("queue: load task with key %q: %w", *task.UniqueKey, err)
		}
	}
	return nil, false, fmt.Errorf("queue: enqueue %s with key %q: key kept changing hands", kind, *task.UniqueKey)
}

// Claim leases the next runnable task of one of kinds (any kind when none
// are given) to workerID. A task is runnable when it is pending and due, or
// running with an expired lease. It returns nil, nil when nothing is ready.
//
// A task whose lease expired on its last attempt is marked dead instead of
// being handed out again.
func (q *Queue) Claim(ctx context.Context, workerID string, kinds ...Kind) (*Task, error) {
	var claimed *Task

	err := q.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for {
			now := q.now()

			query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
				Where("(state = ? AND run_at <= ?) OR (state = ? AND locked_until < ?)",
					models.TaskPending, now, models.TaskRunning, now)
			if len(kinds) > 0 {
				names := make([]string, len(kinds))
				for i, k := range kinds {
					names[i] = string(k)
				}
				query = query.Where("kind IN ?", names)
			}

			var task Task
			err := query.Order("run_at ASC, id ASC").Limit(1).Take(&task).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}

			if task.State == models.TaskRunning && task.Attempts >= task.MaxAttempts {
				if err := tx.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]any{
					"state":        models.TaskDead,
					"locked_by":    "",
					"locked_until": nil,
					"finished_at":  now,
					"last_error": fmt.Sprintf("lease held by %q expired on the last attempt (%d of %d)",
						task.LockedBy, task.Attempts, task.MaxAttempts),
				}).Error; err != nil {
					return err
				}
				continue
			}

			lockedUntil := now.Add(q.opts.VisibilityTimeout)
			if err := tx.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]any{
				"state":        models.TaskRunning,
				"attempts":     gorm.Expr("attempts + 1"),
				"locked_by":    workerID,
				"locked_until": lockedUntil,
			}).Error; err != nil {
				return err
			}

			task.State = models.TaskRunning
			task.Attempts++
			task.LockedBy = workerID
			task.LockedUntil = &lockedUntil
			claimed = &task
			return nil
		}
	})
	if err != nil {
		return nil, fmt.Errorf("queue: claim: %w", err)
	}
	return claimed, nil
}

// fenced scopes an update to the attempt of task that the caller holds.
func (q *Queue) fenced(ctx context.Context, task *Task) *gorm.DB {
	return q.db.WithContext(ctx).Model(&Task{}).
		Where("id = ? AND state = ? AND attempts = ?", task.ID, models.TaskRunning, task.Attempts)
}

// Complete marks a claimed task as done.
func (q *Queue) Complete(ctx context.Context, task *Task) error {
	now := q.now()
	res := q.fenced(ctx, task).Updates(map[string]any{
		"state":        models.TaskCompleted,
		"locked_until": nil,
		"finished_at":  now,
		"last_error":   "",
	})
	if res.Error != nil {
		return fmt.Errorf("queue: complete: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	task.State = models.TaskCompleted
	task.FinishedAt = &now
	return nil
}

// Fail records that a claimed task's attempt failed with cause. The task is
// scheduled for a retry after an exponential backoff (or the delay from
// RetryAfter), or marked dead when it is out of attempts or cause is
// Permanent.
func (q *Queue) Fail(ctx context.Context, task *Task, cause error) error {
	now := q.now()
	msg := "unknown error"
	if cause != nil {
		msg = cause.Error()
	}

	updates := map[string]any{
		"locked_until": nil,
		"last_error":   msg,
	}

	var permanent *permanentError
	if errors.As(cause, &permanent) || task.Attempts >= task.MaxAttempts {
		updates["state"] = models.TaskDead
		updates["finished_at"] = now
	} else {
		delay := q.Backoff(task.Attempts)
		var retry *retryAfterError
		if errors.As(cause, &retry) {
			delay = retry.delay
		}
		updates["state"] = models.TaskPending
		updates["run_at"] = now.Add(delay)
	}

	res := q.fenced(ctx, task).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("queue: fail: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	task.State = updates["state"].(string)
	task.LastError = msg
	if runAt, ok := updates["run_at"].(time.Time); ok {
		task.RunAt = runAt
	}
	return nil
}

// Extend renews the lease on a claimed task.
func (q *Queue) Extend(ctx context.Context, task *Task) error {
	lockedUntil := q.now().Add(q.opts.VisibilityTimeout)
	res := q.fenced(ctx, task).Update("locked_until", lockedUntil)
	if res.Error != nil {
		return fmt.Errorf("queue: extend: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	task.LockedUntil = &lockedUntil
	return nil
}

// Backoff returns the retry delay after the given attempt (1-based):
// BaseBackoff doubled for each earlier attempt, capped at MaxBackoff.
func (q *Queue) Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := q.opts.BaseBackoff
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= q.opts.MaxBackoff {
			return q.opts.MaxBackoff
		}
	}
	return min(delay, q.opts.MaxBackoff)
}

// Get loads a task by id.
func (q *Queue) Get(ctx context.Context, id any) (*Task, error) {
	var task Task
	if err := q.db.WithContext(ctx).Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// Decode unmarshals a task's payload into v.
func Decode(task *Task, v any) error {
	if err := json.Unmarshal([]byte(task.Payload), v); err != nil {
		return Permanent(fmt.Errorf("decode %s payload: %w", task.Kind, err))
	}
	return nil
}

type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks err as not worth retrying: the task goes straight to dead.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

type retryAfterError struct {
	err   error
	delay time.Duration
}

func (e *retryAfterError) Error() string { return e.err.Error() }
func (e *retryAfterError) Unwrap() error { return e.err }

// RetryAfter asks for the next attempt after delay instead of the usual
// backoff, e.g. when a provider is rate limiting. It still uses an attempt.
func RetryAfter(err error, delay time.Duration) error {
	if err == nil {
		return nil
	}
	return &retryAfterError{err: err, delay: delay}
}
