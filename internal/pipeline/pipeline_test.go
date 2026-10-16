package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/matcher"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two users' searches find overlapping postings on two portals. Each
// posting is stored and embedded once, each search gets its own results,
// and every result is evaluated once. Running everything again changes
// nothing but the new posting.
func TestPipeline_EndToEnd_IdempotentAcrossRuns(t *testing.T) {
	userA, userB := ulid.Make(), ulid.Make()
	searchA, searchB := newSearch(userA, "go engineer"), newSearch(userB, "backend")
	searchB.Frequency = FrequencyHourly

	fetcher := newFakeFetcher("linkedin", "jobindex")
	fetcher.setPosts("linkedin", post("linkedin", "L1", ""), post("linkedin", "L2", ""))
	fetcher.setPosts("jobindex", post("jobindex", "J1", "inline description"))
	fetcher.details["linkedin/L1"] = "L1 description"
	fetcher.details["linkedin/L2"] = "L2 description"

	h := newHarness(t, newFakeSearches(searchA, searchB), fetcher)
	cvA := seedCV(t, userA, time.Now())
	cvB := seedCV(t, userB, time.Now())

	start := h.clock.Now()
	assert.Equal(t, 2, h.tick())
	h.drain()

	assert.Equal(t, int64(3), count(t, &models.Job{}), "postings are global")
	assert.Equal(t, int64(3), count(t, &models.Job{}, "status = ?", models.JobStatusEmbedded))
	assert.Equal(t, int64(6), count(t, &models.SearchResult{}))
	assert.Equal(t, int64(3), count(t, &models.SearchResult{}, "job_search_id = ?", searchA.ID))
	assert.Equal(t, int64(6), count(t, &models.SearchResult{}, "evaluation_status = ?", models.EvaluationDone))
	assert.Equal(t, int64(6), count(t, &models.Evaluation{}, "status = ?", models.StatusCompleted))
	assert.Equal(t, int64(3), count(t, &models.Evaluation{}, "cv_id = ?", cvA.ID))
	assert.Equal(t, int64(3), count(t, &models.Evaluation{}, "cv_id = ?", cvB.ID))
	assert.Equal(t, int64(12), count(t, &models.EvaluationScore{}), "two scored criteria per evaluation")
	assert.Equal(t, int32(3), h.embedder.calls.Load(), "each job embedded once")
	assert.Equal(t, 2, fetcher.totalDetailCalls(), "details fetched only when missing, once per job")
	assert.Equal(t, 6, h.evaluator.callCount())

	a := h.searches.get(searchA.ID)
	require.NotNil(t, a.LastRunAt)
	assert.Equal(t, start, *a.LastRunAt)
	assert.Equal(t, start.Add(24*time.Hour), *a.NextRunAt)
	assert.Equal(t, start.Add(time.Hour), *h.searches.get(searchB.ID).NextRunAt)

	var eval models.Evaluation
	require.NoError(t, testDB.Where("cv_id = ?", cvA.ID).First(&eval).Error)
	assert.Equal(t, 77, eval.OverallScore)
	assert.Equal(t, models.StringList{"go"}, eval.Strengths)
	assert.Equal(t, models.StringList{"rust"}, eval.MissingSkills)
	assert.Equal(t, goodRaw, eval.RawResponse)

	var evaluated models.SearchResult
	require.NoError(t, testDB.Take(&evaluated, "id = ?", eval.SearchResultID).Error)
	require.NotNil(t, evaluated.EvaluationID, "the result points at its current evaluation")
	assert.Equal(t, eval.ID, *evaluated.EvaluationID)
	require.NotNil(t, evaluated.OverallScore)
	assert.Equal(t, 77, *evaluated.OverallScore, "the score is copied onto the result")
	assert.Equal(t, DefaultModel, eval.Model)
	assert.Equal(t, DefaultPromptVersion, eval.PromptVersion)

	var scores []models.EvaluationScore
	require.NoError(t, testDB.Where("evaluation_id = ?", eval.ID).Order("criterion").Find(&scores).Error)
	require.Len(t, scores, 2)
	assert.Equal(t, "domain", scores[0].Criterion)
	assert.Equal(t, 90, scores[0].Score)
	assert.Equal(t, "logistics background", scores[0].Reason)
	assert.Equal(t, "skills", scores[1].Criterion)

	// Not due yet: nothing happens.
	assert.Equal(t, 0, h.tick())

	// Both searches due again, and a new posting appears on LinkedIn.
	fetcher.setPosts("linkedin", post("linkedin", "L1", ""), post("linkedin", "L2", ""), post("linkedin", "L3", "L3 inline"))
	h.clock.Advance(25 * time.Hour)
	assert.Equal(t, 2, h.tick())
	h.drain()

	assert.Equal(t, 4, fetcher.searchCount("linkedin"), "each of the two searches ran twice on LinkedIn")
	assert.Equal(t, int64(4), count(t, &models.Job{}))
	assert.Equal(t, int64(8), count(t, &models.SearchResult{}))
	assert.Equal(t, int64(8), count(t, &models.Evaluation{}))
	assert.Equal(t, int32(4), h.embedder.calls.Load(), "only the new job is embedded")
	assert.Equal(t, 8, h.evaluator.callCount(), "only the new results are evaluated")
	assert.Equal(t, 2, fetcher.totalDetailCalls())
}

func TestEmbedOne_ConcurrentClaimersNeverShareAJob(t *testing.T) {
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	h.embedder.delay = 2 * time.Millisecond

	const n = 40
	for i := range n {
		require.NoError(t, testDB.Create(&models.Job{
			Provider: "linkedin", JobID: fmt.Sprint(i), Detail: fmt.Sprintf("description %d", i),
		}).Error)
	}

	var (
		wg     sync.WaitGroup
		perWkr [2]atomic.Int32
	)
	for w := range 2 {
		wg.Go(func() {
			for {
				found, err := h.pipeline.EmbedOne(t.Context())
				if !assert.NoError(t, err) || !found {
					return
				}
				perWkr[w].Add(1)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, int32(n), perWkr[0].Load()+perWkr[1].Load())
	assert.Positive(t, perWkr[0].Load(), "both claimers make progress")
	assert.Positive(t, perWkr[1].Load(), "both claimers make progress")
	assert.Len(t, h.embedder.texts, n)
	for text, c := range h.embedder.texts {
		assert.Equal(t, 1, c, "%q embedded once", text)
	}
	assert.Equal(t, int64(n), count(t, &models.Job{}, "status = ? AND attempts = 1", models.JobStatusEmbedded))
}

func TestEvaluateOne_ConcurrentClaimersNeverShareAResult(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	seedCV(t, user, time.Now())
	h.evaluator.delay = 2 * time.Millisecond
	const n = 20
	for i := range n {
		seedEmbeddedJob(t, search, "linkedin", fmt.Sprint(i))
	}

	var wg sync.WaitGroup
	var perWkr [2]atomic.Int32
	for w := range 2 {
		wg.Go(func() {
			for {
				found, err := h.pipeline.EvaluateOne(t.Context())
				if !assert.NoError(t, err) || !found {
					return
				}
				perWkr[w].Add(1)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, n, h.evaluator.callCount(), "each result evaluated once")
	assert.Positive(t, perWkr[0].Load())
	assert.Positive(t, perWkr[1].Load())
	assert.Equal(t, int64(n), count(t, &models.Evaluation{}))
}

func TestEmbedOne_ReclaimsStaleProcessingJob(t *testing.T) {
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	job := &models.Job{Provider: "linkedin", JobID: "1", Detail: "desc"}
	require.NoError(t, testDB.Create(job).Error)

	// A worker claims the job and dies.
	now := h.clock.Now()
	claimed, err := claimJob(testDB, now, now.Add(-h.pipeline.cfg.StaleTimeout))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, models.JobStatusProcessing, claimed.Status)
	assert.Equal(t, 1, claimed.Attempts)

	found, err := h.pipeline.EmbedOne(t.Context())
	require.NoError(t, err)
	assert.False(t, found, "a fresh claim is not taken over")

	h.clock.Advance(DefaultStaleTimeout + time.Second)
	found, err = h.pipeline.EmbedOne(t.Context())
	require.NoError(t, err)
	assert.True(t, found, "a claim older than the stale timeout is reclaimed")

	var got models.Job
	require.NoError(t, testDB.Take(&got, "id = ?", job.ID).Error)
	assert.Equal(t, models.JobStatusEmbedded, got.Status)
	assert.Equal(t, 2, got.Attempts)
	assert.Nil(t, got.ClaimedAt)
}

func TestEmbedOne_FailureBacksOffThenFails(t *testing.T) {
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	h.embedder.err = errors.New("ollama down")
	job := &models.Job{Provider: "linkedin", JobID: "1", Detail: "desc"}
	require.NoError(t, testDB.Create(job).Error)

	delays := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute}
	for i, delay := range delays {
		found, err := h.pipeline.EmbedOne(t.Context())
		require.NoError(t, err)
		require.True(t, found, "attempt %d", i+1)

		var got models.Job
		require.NoError(t, testDB.Take(&got, "id = ?", job.ID).Error)
		assert.Equal(t, models.JobStatusDescribed, got.Status, "back to its ready state")
		assert.Equal(t, i+1, got.Attempts)
		assert.Contains(t, got.LastError, "ollama down")
		require.NotNil(t, got.NextAttemptAt)
		assert.WithinDuration(t, h.clock.Now().Add(delay), *got.NextAttemptAt, time.Millisecond)

		found, err = h.pipeline.EmbedOne(t.Context())
		require.NoError(t, err)
		assert.False(t, found, "not retried before the backoff")
		h.clock.Advance(delay)
	}

	found, err := h.pipeline.EmbedOne(t.Context())
	require.NoError(t, err)
	require.True(t, found)

	var got models.Job
	require.NoError(t, testDB.Take(&got, "id = ?", job.ID).Error)
	assert.Equal(t, models.JobStatusFailed, got.Status)
	assert.Equal(t, DefaultMaxAttempts, got.Attempts)

	h.clock.Advance(time.Hour)
	found, err = h.pipeline.EmbedOne(t.Context())
	require.NoError(t, err)
	assert.False(t, found, "failed jobs are not retried")
}

func TestEmbedOne_KeepsFetchedDetailWhenEmbeddingFails(t *testing.T) {
	fetcher := newFakeFetcher("linkedin")
	fetcher.details["linkedin/1"] = "fetched description"
	h := newHarness(t, newFakeSearches(), fetcher)
	h.embedder.err = errors.New("ollama down")
	job := &models.Job{Provider: "linkedin", JobID: "1"}
	require.NoError(t, testDB.Create(job).Error)

	_, err := h.pipeline.EmbedOne(t.Context())
	require.NoError(t, err)

	var got models.Job
	require.NoError(t, testDB.Take(&got, "id = ?", job.ID).Error)
	assert.Equal(t, models.JobStatusDescribed, got.Status)
	assert.Equal(t, "fetched description", got.Detail)

	// The retry only embeds; it does not fetch again.
	h.embedder.err = nil
	h.clock.Advance(time.Hour)
	_, err = h.pipeline.EmbedOne(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, fetcher.totalDetailCalls())
}

func TestEmbedOne_NoDescriptionIsPermanent(t *testing.T) {
	fetcher := newFakeFetcher("linkedin")
	fetcher.details["linkedin/1"] = "   "
	h := newHarness(t, newFakeSearches(), fetcher)
	job := &models.Job{Provider: "linkedin", JobID: "1"}
	require.NoError(t, testDB.Create(job).Error)

	_, err := h.pipeline.EmbedOne(t.Context())
	require.NoError(t, err)

	var got models.Job
	require.NoError(t, testDB.Take(&got, "id = ?", job.ID).Error)
	assert.Equal(t, models.JobStatusFailed, got.Status)
	assert.Equal(t, 1, got.Attempts)
	assert.Equal(t, int32(0), h.embedder.calls.Load())
}

func TestEmbedOne_ShutdownReleasesJob(t *testing.T) {
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	h.embedder.delay = time.Minute
	job := &models.Job{Provider: "linkedin", JobID: "1", Detail: "desc"}
	require.NoError(t, testDB.Create(job).Error)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	found, err := h.pipeline.EmbedOne(ctx)
	require.NoError(t, err)
	assert.True(t, found)

	var got models.Job
	require.NoError(t, testDB.Take(&got, "id = ?", job.ID).Error)
	assert.Equal(t, models.JobStatusDescribed, got.Status, "released, not left processing")
	assert.Equal(t, 0, got.Attempts, "the interrupted attempt does not count")
	assert.Nil(t, got.ClaimedAt)
}

func TestEvaluateOne_WaitsForEmbedding(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	seedCV(t, user, time.Now())

	job := &models.Job{Provider: "linkedin", JobID: "1", Detail: "desc"}
	require.NoError(t, testDB.Create(job).Error)
	require.NoError(t, upsertSearchResult(testDB, search, job.ID))

	found, err := h.pipeline.EvaluateOne(t.Context())
	require.NoError(t, err)
	assert.False(t, found, "results of unembedded jobs are not claimed")

	h.drain()
	assert.Equal(t, int64(1), count(t, &models.Evaluation{}, "status = ?", models.StatusCompleted))
}

func TestEvaluateOne_SkipsUserWithoutCVThenEvaluatesLater(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	_, result := seedEmbeddedJob(t, search, "linkedin", "1")

	found, err := h.pipeline.EvaluateOne(t.Context())
	require.NoError(t, err)
	require.True(t, found)

	var got models.SearchResult
	require.NoError(t, testDB.Take(&got, "id = ?", result.ID).Error)
	assert.Equal(t, models.EvaluationPending, got.EvaluationStatus)
	assert.Equal(t, 0, got.Attempts, "no CV does not use up attempts")
	assert.Equal(t, "user has no CV", got.LastError)
	require.NotNil(t, got.NextAttemptAt)
	assert.WithinDuration(t, h.clock.Now().Add(DefaultNoCVRetry), *got.NextAttemptAt, time.Millisecond)
	assert.Equal(t, 0, h.evaluator.callCount())
	assert.Equal(t, int64(0), count(t, &models.Evaluation{}))

	seedCV(t, user, time.Now())
	h.clock.Advance(DefaultNoCVRetry)
	h.drain()
	assert.Equal(t, int64(1), count(t, &models.Evaluation{}, "status = ?", models.StatusCompleted))
}

func TestEvaluateOne_UsesNewestCV(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	seedCV(t, user, time.Now().Add(-time.Hour))
	newest := seedCV(t, user, time.Now())
	seedCV(t, ulid.Make(), time.Now().Add(time.Hour)) // another user's
	seedEmbeddedJob(t, search, "linkedin", "1")

	h.drain()
	require.Equal(t, 1, h.evaluator.callCount())
	assert.Equal(t, newest.ID, h.evaluator.cvs[0])
}

func TestEvaluateOne_FailureRecordedThenRetried(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	seedCV(t, user, time.Now())
	_, result := seedEmbeddedJob(t, search, "linkedin", "1")

	h.evaluator.fail = func(call int) (string, error) {
		if call == 1 {
			return "not json", errors.New("failed to decode LLM response")
		}
		return "", nil
	}

	h.drain()
	var eval models.Evaluation
	require.NoError(t, testDB.Take(&eval).Error)
	assert.Equal(t, models.StatusFailed, eval.Status)
	assert.Equal(t, "not json", eval.RawResponse)
	var got models.SearchResult
	require.NoError(t, testDB.Take(&got, "id = ?", result.ID).Error)
	assert.Equal(t, models.EvaluationPending, got.EvaluationStatus)
	assert.Equal(t, 1, got.Attempts)

	h.clock.Advance(time.Hour)
	h.drain()

	var evals []models.Evaluation
	require.NoError(t, testDB.Find(&evals).Error)
	require.Len(t, evals, 1, "the retry overwrites the failed evaluation")
	assert.Equal(t, eval.ID, evals[0].ID)
	assert.Equal(t, models.StatusCompleted, evals[0].Status)
	assert.Empty(t, evals[0].Error)
	assert.Equal(t, goodRaw, evals[0].RawResponse)
	assert.Equal(t, int64(2), count(t, &models.EvaluationScore{}))
	require.NoError(t, testDB.Take(&got, "id = ?", result.ID).Error)
	assert.Equal(t, models.EvaluationDone, got.EvaluationStatus)
}

func TestEvaluateOne_PersistentFailureEndsFailed(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	seedCV(t, user, time.Now())
	_, result := seedEmbeddedJob(t, search, "linkedin", "1")
	h.evaluator.fail = func(int) (string, error) {
		return "garbage", errors.New("failed to decode LLM response: invalid character")
	}

	h.drainWithRetries()

	var got models.SearchResult
	require.NoError(t, testDB.Take(&got, "id = ?", result.ID).Error)
	assert.Equal(t, models.EvaluationFailed, got.EvaluationStatus)
	assert.Equal(t, DefaultMaxAttempts, got.Attempts)
	assert.Contains(t, got.LastError, "invalid character")
	assert.Equal(t, DefaultMaxAttempts, h.evaluator.callCount())

	var eval models.Evaluation
	require.NoError(t, testDB.Take(&eval).Error)
	assert.Equal(t, models.StatusFailed, eval.Status)
	assert.Contains(t, eval.Error, "invalid character")
	assert.Equal(t, "garbage", eval.RawResponse)
	require.NotNil(t, got.EvaluationID)
	assert.Equal(t, eval.ID, *got.EvaluationID)
	assert.Nil(t, got.OverallScore, "a failed evaluation has no score")
}

func TestEvaluateOne_ReevaluationReplacesScores(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	seedCV(t, user, time.Now())
	_, result := seedEmbeddedJob(t, search, "linkedin", "1")

	h.drain()
	var first models.Evaluation
	require.NoError(t, testDB.Take(&first).Error)
	assert.Equal(t, int64(2), count(t, &models.EvaluationScore{}, "evaluation_id = ?", first.ID))

	// Re-evaluate: set the result back to pending.
	h.evaluator.resp = &matcher.Response{
		OverallScore: 40,
		Breakdown:    []matcher.CriterionScore{{Criterion: "seniority", Score: 30, Reason: "junior"}},
	}
	require.NoError(t, testDB.Model(&models.SearchResult{}).Where("id = ?", result.ID).
		Updates(map[string]any{"evaluation_status": models.EvaluationPending, "attempts": 0}).Error)
	h.drain()

	var second models.Evaluation
	require.NoError(t, testDB.Take(&second).Error)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, 40, second.OverallScore)
	var reevaluated models.SearchResult
	require.NoError(t, testDB.Take(&reevaluated, "id = ?", result.ID).Error)
	require.NotNil(t, reevaluated.OverallScore)
	assert.Equal(t, 40, *reevaluated.OverallScore, "the copied score follows the new evaluation")
	var scores []models.EvaluationScore
	require.NoError(t, testDB.Where("evaluation_id = ?", second.ID).Find(&scores).Error)
	require.Len(t, scores, 1, "the old scores are replaced, not merged")
	assert.Equal(t, "seniority", scores[0].Criterion)
	assert.Equal(t, 30, scores[0].Score)
}

func TestRun_CapsConcurrentEvaluations(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"),
		func(c *Config) { c.EvaluateWorkers = 2; c.EmbedWorkers = 1 })
	seedCV(t, user, time.Now())
	for i := range 6 {
		seedEmbeddedJob(t, search, "linkedin", fmt.Sprint(i))
	}
	h.evaluator.delay = 100 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() { h.pipeline.Run(ctx) })
	require.Eventually(t, func() bool {
		return count(t, &models.Evaluation{}, "status = ?", models.StatusCompleted) == 6
	}, 20*time.Second, 20*time.Millisecond)
	cancel()
	wg.Wait()

	assert.Equal(t, int32(2), h.evaluator.maxSeen.Load(), "never more than EvaluateWorkers evaluations at once")
}

type recordingObserver struct {
	mu       sync.Mutex
	started  map[string]int
	finished map[string]int
	errs     []error
}

type ctxKey struct{}

func (o *recordingObserver) RunStarted(ctx context.Context, kind string) context.Context {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.started == nil {
		o.started, o.finished = map[string]int{}, map[string]int{}
	}
	o.started[kind]++
	return context.WithValue(ctx, ctxKey{}, kind)
}

func (o *recordingObserver) RunFinished(ctx context.Context, kind string, _ time.Duration, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if ctx.Value(ctxKey{}) != kind {
		panic("RunFinished did not get the context from RunStarted")
	}
	o.finished[kind]++
	if err != nil {
		o.errs = append(o.errs, err)
	}
}

type ctxCheckingFetcher struct {
	*fakeFetcher
	mu    sync.Mutex
	kinds []string
}

func (f *ctxCheckingFetcher) Search(ctx context.Context, provider string, c Criteria) ([]entities.JobPost, error) {
	f.mu.Lock()
	if k, ok := ctx.Value(ctxKey{}).(string); ok {
		f.kinds = append(f.kinds, k)
	}
	f.mu.Unlock()
	return f.fakeFetcher.Search(ctx, provider, c)
}

func TestObserver_WrapsStepsAndTicks(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	inner := newFakeFetcher("linkedin", "jobindex")
	inner.setPosts("linkedin", post("linkedin", "1", "d"))
	inner.searchErr["jobindex"] = errors.New("jobindex down")
	fetcher := &ctxCheckingFetcher{fakeFetcher: inner}
	obs := &recordingObserver{}

	h := newHarness(t, newFakeSearches(search), inner, func(c *Config) {
		c.Fetcher = fetcher
		c.Observer = obs
	})
	seedCV(t, user, time.Now())

	h.tick()
	h.drain()

	assert.Equal(t, map[string]int{
		KindScheduleTick: 1,
		KindRunSearch:    2, // one per portal
		KindEmbedJob:     1,
		KindEvaluateJob:  1,
	}, obs.started)
	assert.Equal(t, obs.started, obs.finished)
	require.Len(t, obs.errs, 1)
	assert.ErrorContains(t, obs.errs[0], "jobindex down")
	assert.ElementsMatch(t, []string{KindRunSearch, KindRunSearch, KindRunSearch, KindRunSearch},
		fetcher.kinds, "portal calls run on the observer's run_search context")
}

func TestNew_RequiresDependencies(t *testing.T) {
	_, err := New(Config{})
	require.Error(t, err)
	for _, name := range []string{"DB", "Searches", "Fetcher", "Embedder", "Evaluator"} {
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
