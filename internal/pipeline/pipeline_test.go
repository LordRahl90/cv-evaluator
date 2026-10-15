package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/queue"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two users' searches find overlapping postings on two portals. Each
// posting is stored and embedded once, each search gets its own results,
// and every result is evaluated once. Running everything again changes
// nothing.
func TestPipeline_EndToEnd_IdempotentAcrossRuns(t *testing.T) {
	userA, userB := ulid.Make(), ulid.Make()
	searchA, searchB := newSearch(userA, "go engineer"), newSearch(userB, "backend")
	searchB.Frequency = FrequencyHourly

	fetcher := newFakeFetcher("linkedin", "jobindex")
	fetcher.posts["linkedin"] = []entities.JobPost{post("linkedin", "L1", ""), post("linkedin", "L2", "")}
	fetcher.posts["jobindex"] = []entities.JobPost{post("jobindex", "J1", "inline description")}
	fetcher.details["linkedin/L1"] = "L1 description"
	fetcher.details["linkedin/L2"] = "L2 description"

	h := newHarness(t, newFakeSearches(searchA, searchB), fetcher)
	cvA := seedCV(t, userA, time.Now())
	cvB := seedCV(t, userB, time.Now())

	start := h.clock.Now()
	assert.Equal(t, 4, h.tick(), "one run_search per (search, portal)")
	h.drain()

	assert.Equal(t, int64(3), count(t, &models.Job{}), "postings are global")
	assert.Equal(t, int64(6), count(t, &models.SearchResult{}))
	assert.Equal(t, int64(3), count(t, &models.SearchResult{}, "job_search_id = ?", searchA.ID))
	assert.Equal(t, int64(3), count(t, &models.SearchResult{}, "user_id = ?", userB))
	assert.Equal(t, int64(6), count(t, &models.Evaluation{}, "status = ?", models.StatusCompleted))
	assert.Equal(t, int64(3), count(t, &models.Evaluation{}, "cv_id = ?", cvA.ID))
	assert.Equal(t, int64(3), count(t, &models.Evaluation{}, "cv_id = ?", cvB.ID))
	assert.Equal(t, int32(3), h.embedLLM.calls.Load(), "each job embedded once")
	assert.Equal(t, 2, fetcher.totalDetailCalls(), "details fetched only when missing, once per job")
	assert.Equal(t, 6, h.evaluator.callCount())
	assert.Equal(t, int64(0), count(t, &models.QueueTask{}, "state <> ?", models.TaskCompleted))

	a := h.searches.get(searchA.ID)
	require.NotNil(t, a.LastRunAt)
	assert.Equal(t, start, *a.LastRunAt)
	assert.Equal(t, start.Add(24*time.Hour), *a.NextRunAt)
	b := h.searches.get(searchB.ID)
	assert.Equal(t, start.Add(time.Hour), *b.NextRunAt)

	var eval models.Evaluation
	require.NoError(t, testDB.Where("cv_id = ?", cvA.ID).First(&eval).Error)
	assert.Equal(t, 77, eval.OverallScore)
	assert.Equal(t, models.StringList{"go"}, eval.Strengths)
	assert.Equal(t, models.StringList{"rust"}, eval.MissingSkills)
	assert.Equal(t, goodRaw, eval.RawResponse)
	assert.Equal(t, DefaultModel, eval.Model)
	assert.Equal(t, DefaultPromptVersion, eval.PromptVersion)

	// Not due yet: nothing happens.
	assert.Equal(t, 0, h.tick())

	// Both searches due again, and a new posting appears on LinkedIn.
	fetcher.posts["linkedin"] = append(fetcher.posts["linkedin"], post("linkedin", "L3", "L3 inline"))
	h.clock.Advance(25 * time.Hour)
	assert.Equal(t, 4, h.tick())
	h.drain()

	assert.Equal(t, 4, fetcher.searchCount("linkedin"), "each of the two searches ran twice on LinkedIn")
	assert.Equal(t, int64(4), count(t, &models.Job{}))
	assert.Equal(t, int64(8), count(t, &models.SearchResult{}))
	assert.Equal(t, int64(8), count(t, &models.Evaluation{}))
	assert.Equal(t, int32(4), h.embedLLM.calls.Load(), "only the new job is embedded")
	assert.Equal(t, 8, h.evaluator.callCount(), "only the new results are evaluated")
	assert.Equal(t, 2, fetcher.totalDetailCalls())
}

func TestRunSearch_RerunningTaskDoesNotDuplicate(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.posts["linkedin"] = []entities.JobPost{post("linkedin", "1", "d1"), post("linkedin", "2", "d2")}
	h := newHarness(t, newFakeSearches(search), fetcher)

	task := &queue.Task{Kind: string(queue.KindRunSearch), Payload: `{"search_id":"` + search.ID.String() + `","provider":"linkedin"}`}
	for range 3 {
		require.NoError(t, h.pipeline.RunSearch(t.Context(), task))
	}

	assert.Equal(t, int64(2), count(t, &models.Job{}))
	assert.Equal(t, int64(2), count(t, &models.SearchResult{}))
	assert.Equal(t, int64(2), count(t, &models.QueueTask{}, "kind = ?", queue.KindEmbedJob),
		"embed tasks are deduped by unique key")
	assert.Equal(t, []Criteria{
		{Query: "go", Location: "Copenhagen", MaxPages: DefaultMaxPages},
		{Query: "go", Location: "Copenhagen", MaxPages: DefaultMaxPages},
		{Query: "go", Location: "Copenhagen", MaxPages: DefaultMaxPages},
	}, fetcher.criteria)

	// Results start as new.
	var r models.SearchResult
	require.NoError(t, testDB.First(&r).Error)
	assert.Equal(t, models.SearchResultNew, r.Status)
	assert.Equal(t, user, r.UserID)
}

func TestRunSearch_SkipsMissingOrDisabledSearch(t *testing.T) {
	disabled := newSearch(ulid.Make(), "x")
	disabled.Enabled = false
	fetcher := newFakeFetcher("linkedin")
	h := newHarness(t, newFakeSearches(disabled), fetcher)

	for _, id := range []ulid.ULID{disabled.ID, ulid.Make()} {
		_, _, err := h.queue.Enqueue(t.Context(), queue.KindRunSearch, RunSearchPayload{SearchID: id, Provider: "linkedin"})
		require.NoError(t, err)
	}
	h.drain()

	assert.Equal(t, 0, fetcher.searchCount("linkedin"))
	assert.Equal(t, int64(2), count(t, &models.QueueTask{}, "state = ?", models.TaskCompleted))
}

func TestRunSearch_OnePortalFailingDoesNotBlockOthers(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	fetcher := newFakeFetcher("linkedin", "jobindex")
	fetcher.posts["linkedin"] = []entities.JobPost{post("linkedin", "1", "d")}
	fetcher.searchErr["jobindex"] = errors.New("jobindex is down")
	h := newHarness(t, newFakeSearches(search), fetcher)
	seedCV(t, user, time.Now())

	h.tick()
	h.drain()

	assert.Equal(t, int64(1), count(t, &models.Evaluation{}, "status = ?", models.StatusCompleted))
	var dead models.QueueTask
	require.NoError(t, testDB.Where("kind = ?", queue.KindRunSearch).Where("state = ?", models.TaskDead).First(&dead).Error)
	assert.Contains(t, dead.Payload, "jobindex")
	assert.Contains(t, dead.LastError, "jobindex is down")
	assert.Equal(t, queue.DefaultMaxAttempts, dead.Attempts)
}

func TestEmbedJob_NoDescriptionIsPermanent(t *testing.T) {
	fetcher := newFakeFetcher("linkedin")
	fetcher.details["linkedin/1"] = "   "
	h := newHarness(t, newFakeSearches(), fetcher)

	job := &models.Job{Provider: "linkedin", JobID: "1", Status: models.StatusPending}
	require.NoError(t, testDB.Create(job).Error)
	_, _, err := h.queue.Enqueue(t.Context(), queue.KindEmbedJob, EmbedJobPayload{JobID: job.ID})
	require.NoError(t, err)
	h.drain()

	var task models.QueueTask
	require.NoError(t, testDB.Where("kind = ?", queue.KindEmbedJob).First(&task).Error)
	assert.Equal(t, models.TaskDead, task.State)
	assert.Equal(t, 1, task.Attempts)
	assert.Equal(t, int32(0), h.embedLLM.calls.Load())
}

func TestEvaluateJob_SkipsUserWithoutCV(t *testing.T) {
	search := newSearch(ulid.Make(), "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.posts["linkedin"] = []entities.JobPost{post("linkedin", "1", "d")}
	h := newHarness(t, newFakeSearches(search), fetcher)

	h.tick()
	h.drain()

	assert.Equal(t, int64(1), count(t, &models.SearchResult{}))
	assert.Equal(t, int64(0), count(t, &models.Evaluation{}))
	assert.Equal(t, 0, h.evaluator.callCount())
	assert.Equal(t, int64(0), count(t, &models.QueueTask{}, "state <> ?", models.TaskCompleted))
}

func TestEvaluateJob_UsesNewestCV(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.posts["linkedin"] = []entities.JobPost{post("linkedin", "1", "d")}
	h := newHarness(t, newFakeSearches(search), fetcher)
	seedCV(t, user, time.Now().Add(-time.Hour))
	newest := seedCV(t, user, time.Now())
	seedCV(t, ulid.Make(), time.Now().Add(time.Hour)) // another user's

	h.tick()
	h.drain()

	require.Equal(t, 1, h.evaluator.callCount())
	assert.Equal(t, newest.ID, h.evaluator.cvs[0])
}

func TestEvaluateJob_FailureRecordedThenRetried(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.posts["linkedin"] = []entities.JobPost{post("linkedin", "1", "d")}
	h := newHarness(t, newFakeSearches(search), fetcher)
	seedCV(t, user, time.Now())

	h.evaluator.fail = func(call int) (string, error) {
		if call == 1 {
			return "not json", errors.New("failed to decode LLM response")
		}
		return "", nil
	}

	h.tick()
	h.drain()

	var evals []models.Evaluation
	require.NoError(t, testDB.Find(&evals).Error)
	require.Len(t, evals, 1, "the retry overwrites the failed evaluation")
	assert.Equal(t, models.StatusCompleted, evals[0].Status)
	assert.Empty(t, evals[0].Error)
	assert.Equal(t, goodRaw, evals[0].RawResponse)
	assert.Equal(t, 2, h.evaluator.callCount())
}

func TestEvaluateJob_PersistentFailureStoredAsFailed(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.posts["linkedin"] = []entities.JobPost{post("linkedin", "1", "d")}
	h := newHarness(t, newFakeSearches(search), fetcher)
	seedCV(t, user, time.Now())

	h.evaluator.fail = func(int) (string, error) {
		return "garbage", errors.New("failed to decode LLM response: invalid character")
	}

	h.tick()
	h.drain()

	var eval models.Evaluation
	require.NoError(t, testDB.First(&eval).Error)
	assert.Equal(t, models.StatusFailed, eval.Status)
	assert.Contains(t, eval.Error, "invalid character")
	assert.Equal(t, "garbage", eval.RawResponse)
	assert.Equal(t, queue.DefaultMaxAttempts, h.evaluator.callCount())
	assert.Equal(t, int64(1), count(t, &models.QueueTask{}, "kind = ? AND state = ?", queue.KindEvaluateJob, models.TaskDead))
}

func TestEvaluateJob_QueuesEmbeddingWhenJobNotEmbedded(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	fetcher := newFakeFetcher("linkedin")
	h := newHarness(t, newFakeSearches(search), fetcher)
	seedCV(t, user, time.Now())

	job := &models.Job{Provider: "linkedin", JobID: "1", Detail: "desc", Status: models.StatusPending}
	require.NoError(t, testDB.Create(job).Error)
	result, err := h.pipeline.store.upsertSearchResult(t.Context(), search, job.ID)
	require.NoError(t, err)

	_, _, err = h.queue.Enqueue(t.Context(), queue.KindEvaluateJob, EvaluateJobPayload{SearchResultID: result.ID})
	require.NoError(t, err)
	h.drain()

	assert.Equal(t, int32(1), h.embedLLM.calls.Load())
	assert.Equal(t, 1, h.evaluator.callCount())
	assert.Equal(t, int64(1), count(t, &models.Evaluation{}, "status = ?", models.StatusCompleted))
}

func TestEvaluateJob_RespectsConcurrencyCap(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	fetcher := newFakeFetcher("linkedin")
	for _, id := range []string{"1", "2", "3", "4", "5", "6"} {
		fetcher.posts["linkedin"] = append(fetcher.posts["linkedin"], post("linkedin", id, "d"+id))
	}
	h := newHarness(t, newFakeSearches(search), fetcher, func(c *Config) { c.MaxConcurrentEvaluations = 2 })
	seedCV(t, user, time.Now())
	h.evaluator.delay = 100 * time.Millisecond

	h.tick()
	w := queue.NewWorker(h.queue, h.pipeline.Handlers(), queue.WorkerOptions{Concurrency: 6, PollInterval: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	require.Eventually(t, func() bool {
		return count(t, &models.Evaluation{}, "status = ?", models.StatusCompleted) == 6
	}, 20*time.Second, 20*time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, int32(2), h.evaluator.maxSeen.Load(), "never more than 2 evaluations at once")
}

type recordingObserver struct {
	mu       sync.Mutex
	started  []string
	finished []string
	errs     []error
}

type ctxKey struct{}

func (o *recordingObserver) RunStarted(ctx context.Context, kind string) context.Context {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.started = append(o.started, kind)
	return context.WithValue(ctx, ctxKey{}, kind)
}

func (o *recordingObserver) RunFinished(ctx context.Context, kind string, d time.Duration, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if ctx.Value(ctxKey{}) != kind {
		panic("RunFinished did not get the context from RunStarted")
	}
	o.finished = append(o.finished, kind)
	o.errs = append(o.errs, err)
}

type ctxCheckingFetcher struct {
	*fakeFetcher
	sawKind chan string
}

func (f *ctxCheckingFetcher) Search(ctx context.Context, provider string, c Criteria) ([]entities.JobPost, error) {
	if k, ok := ctx.Value(ctxKey{}).(string); ok {
		f.sawKind <- k
	}
	return f.fakeFetcher.Search(ctx, provider, c)
}

func TestObserver_WrapsHandlersAndTicks(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	inner := newFakeFetcher("linkedin")
	inner.posts["linkedin"] = []entities.JobPost{post("linkedin", "1", "d")}
	fetcher := &ctxCheckingFetcher{fakeFetcher: inner, sawKind: make(chan string, 10)}
	obs := &recordingObserver{}

	resetDB(t)
	q := queue.New(testDB, queue.Options{BaseBackoff: time.Millisecond})
	searches := newFakeSearches(search)
	p, err := New(Config{
		DB: testDB, Queue: q, Searches: searches, Fetcher: fetcher,
		Embedder: embedderFunc(func(context.Context, *models.Job) error { return errors.New("embed down") }),
		Evaluator: &fakeEvaluator{}, Observer: obs,
		RateLimit: RateLimitOptions{Interval: time.Microsecond},
	})
	require.NoError(t, err)
	sched := NewScheduler(q, searches, fetcher, SchedulerOptions{Observer: obs})

	_, err = sched.Tick(t.Context())
	require.NoError(t, err)
	w := queue.NewWorker(q, p.Handlers(), queue.WorkerOptions{})
	for range 2 {
		_, err := w.RunOnce(t.Context())
		require.NoError(t, err)
	}

	assert.Equal(t, []string{KindScheduleTick, string(queue.KindRunSearch), string(queue.KindEmbedJob)}, obs.started)
	assert.Equal(t, obs.started, obs.finished)
	assert.NoError(t, obs.errs[0])
	assert.NoError(t, obs.errs[1])
	assert.ErrorContains(t, obs.errs[2], "embed down")
	assert.Equal(t, string(queue.KindRunSearch), <-fetcher.sawKind, "the handler runs on the observer's context")
}

type embedderFunc func(context.Context, *models.Job) error

func (f embedderFunc) Embed(ctx context.Context, j *models.Job) error { return f(ctx, j) }

func TestNew_RequiresDependencies(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	for _, name := range []string{"DB", "Queue", "Searches", "Fetcher", "Embedder", "Evaluator"} {
		assert.Contains(t, err.Error(), name)
	}
}

func TestLatestCVSelector(t *testing.T) {
	resetDB(t)
	sel := NewLatestCVSelector(testDB)
	user := ulid.Make()

	cv, err := sel.SelectCV(t.Context(), user)
	require.NoError(t, err)
	assert.Nil(t, cv)

	seedCV(t, user, time.Now().Add(-time.Hour))
	newest := seedCV(t, user, time.Now())
	deleted := seedCV(t, user, time.Now().Add(time.Hour))
	require.NoError(t, testDB.Delete(deleted).Error)

	cv, err = sel.SelectCV(t.Context(), user)
	require.NoError(t, err)
	require.NotNil(t, cv)
	assert.Equal(t, newest.ID, cv.ID, "the newest CV that is not deleted")
}
