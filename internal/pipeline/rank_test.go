package pipeline

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	resultsvc "cv-evaluator/internal/services/results"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withDailyCap(n int) harnessOption {
	return func(c *Config) { c.EvalDailyCap = n }
}

func withFloor(f float64) harnessOption {
	return func(c *Config) { c.SimilarityFloor = f }
}

// seedJobWithEmbedding stores an embedded job with embedding v (nil stores
// NULL) and a pending result for search.
func seedJobWithEmbedding(t *testing.T, search *Search, id string, v []float32) (*models.Job, *models.SearchResult) {
	t.Helper()
	job, result := seedEmbeddedJob(t, search, "linkedin", id)
	seedJobEmbedding(t, job.ID, v)
	return job, result
}

func loadResult(t *testing.T, id ulid.ULID) models.SearchResult {
	t.Helper()
	var r models.SearchResult
	require.NoError(t, testDB.Take(&r, "id = ?", id).Error)
	return r
}

func (h *harness) rank() int {
	h.t.Helper()
	n, err := h.pipeline.Rank(h.t.Context())
	require.NoError(h.t, err)
	return n
}

// Results are evaluated most similar first; a result with no similarity is
// evaluated last, even though it is the oldest.
func TestEvaluate_MostSimilarFirstAndNullLast(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	cv := seedCV(t, user, time.Now())
	seedCVEmbedding(t, cv.ID, unit(1))

	unranked, unrankedResult := seedJobWithEmbedding(t, search, "none", nil)
	far, _ := seedJobWithEmbedding(t, search, "far", unit(0, 1))
	near, _ := seedJobWithEmbedding(t, search, "near", unit(1, 1))
	same, sameResult := seedJobWithEmbedding(t, search, "same", unit(1))

	assert.Equal(t, 4, h.rank())
	assert.Equal(t, 0, h.rank(), "nothing changes until the CV does")

	r := loadResult(t, sameResult.ID)
	require.NotNil(t, r.Similarity)
	assert.InDelta(t, 1, *r.Similarity, 1e-6)
	assert.Equal(t, cv.ID, *r.SimilarityCVID)
	r = loadResult(t, unrankedResult.ID)
	assert.Nil(t, r.Similarity)
	assert.Equal(t, cv.ID, *r.SimilarityCVID, "tried against this CV")

	h.drain()
	assert.Equal(t, []ulid.ULID{same.ID, near.ID, far.ID, unranked.ID}, h.evaluator.evaluatedJobs())
	assert.Equal(t, int64(4), count(t, &models.SearchResult{}, "evaluation_status = ?", models.EvaluationDone))
}

func TestRank_NewCVRecomputesSimilarity(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"), withFloor(0.5))
	old := seedCV(t, user, time.Now().Add(-time.Hour))
	seedCVEmbedding(t, old.ID, unit(0, 1))
	_, result := seedJobWithEmbedding(t, search, "1", unit(1))

	// Against the old CV it is unrelated, and the floor holds it.
	found, err := h.pipeline.EvaluateOne(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	r := loadResult(t, result.ID)
	assert.Equal(t, models.EvaluationHeld, r.EvaluationStatus)
	assert.InDelta(t, 0, *r.Similarity, 1e-6)
	assert.Equal(t, 0, h.rank(), "already ranked against the newest CV")

	// A new CV that matches: ranking recomputes it and releases it.
	cv := seedCV(t, user, time.Now())
	seedCVEmbedding(t, cv.ID, unit(1))
	assert.Equal(t, 1, h.rank())
	r = loadResult(t, result.ID)
	assert.Equal(t, models.EvaluationPending, r.EvaluationStatus)
	require.NotNil(t, r.Similarity)
	assert.InDelta(t, 1, *r.Similarity, 1e-6)
	assert.Equal(t, cv.ID, *r.SimilarityCVID)

	h.drain()
	assert.Equal(t, models.EvaluationDone, loadResult(t, result.ID).EvaluationStatus)
	assert.Equal(t, []ulid.ULID{cv.ID}, h.evaluator.cvs)
}

func TestEvaluate_FloorHoldsDissimilarResults(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"), withFloor(0.5))
	cv := seedCV(t, user, time.Now())
	seedCVEmbedding(t, cv.ID, unit(1))
	far, farResult := seedJobWithEmbedding(t, search, "far", unit(0, 1))
	near, _ := seedJobWithEmbedding(t, search, "near", unit(1, 1))
	unranked, _ := seedJobWithEmbedding(t, search, "none", nil)

	h.drain()
	assert.Equal(t, []ulid.ULID{near.ID, unranked.ID}, h.evaluator.evaluatedJobs(),
		"below the floor is not sent to the LLM; no similarity is never held")
	r := loadResult(t, farResult.ID)
	assert.Equal(t, models.EvaluationHeld, r.EvaluationStatus)
	assert.Equal(t, 0, r.Attempts, "holding is not an attempt")
	assert.Nil(t, r.EvaluationID)
	assert.Nil(t, r.EvaluatedAt)

	assert.Equal(t, 0, h.rank(), "the floor still holds it")
	h.drain()
	assert.Equal(t, 2, h.evaluator.callCount())

	// The user asks for it: it is scored regardless of the floor.
	_, err := resultsvc.New(testDB).ReEvaluate(t.Context(), user, farResult.ID)
	require.NoError(t, err)
	h.drain()
	assert.Equal(t, []ulid.ULID{near.ID, unranked.ID, far.ID}, h.evaluator.evaluatedJobs())
	r = loadResult(t, farResult.ID)
	assert.Equal(t, models.EvaluationDone, r.EvaluationStatus)
	assert.False(t, r.Forced, "cleared once evaluated")
}

func TestRank_ReleasesHeldWhenFloorTurnedOff(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"), withFloor(0.5))
	cv := seedCV(t, user, time.Now())
	seedCVEmbedding(t, cv.ID, unit(1))
	_, result := seedJobWithEmbedding(t, search, "far", unit(0, 1))
	h.drain()
	require.Equal(t, models.EvaluationHeld, loadResult(t, result.ID).EvaluationStatus)

	p, err := New(Config{
		DB: testDB, Searches: h.searches, Fetcher: h.fetcher, Embedder: h.embedder,
		Evaluator: h.evaluator, Now: h.clock.Now,
	})
	require.NoError(t, err)
	n, err := p.Rank(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, models.EvaluationPending, loadResult(t, result.ID).EvaluationStatus)
}

// A user at the daily cap gets nothing more evaluated until the window
// moves on; other users are not held up, and the user's own requests go
// through.
func TestEvaluate_DailyCap(t *testing.T) {
	userA, userB := ulid.Make(), ulid.Make()
	searchA, searchB := newSearch(userA, "go"), newSearch(userB, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"), withDailyCap(2))
	seedCV(t, userA, time.Now())
	seedCV(t, userB, time.Now())
	var resultsA []*models.SearchResult
	for i := range 5 {
		_, r := seedEmbeddedJob(t, searchA, "linkedin", fmt.Sprint("a", i))
		resultsA = append(resultsA, r)
	}
	seedEmbeddedJob(t, searchB, "linkedin", "b0")

	h.drain()
	assert.Equal(t, 3, h.evaluator.callCount(), "two for A, one for B")
	waiting := "user_id = ? AND evaluation_status = ?"
	assert.Equal(t, int64(3), count(t, &models.SearchResult{}, waiting, userA, models.EvaluationPending))

	// The user asks for one of the waiting results: the cap does not apply.
	var asked *models.SearchResult
	for _, r := range resultsA {
		if loadResult(t, r.ID).EvaluationStatus == models.EvaluationPending {
			asked = r
			break
		}
	}
	_, err := resultsvc.New(testDB).ReEvaluate(t.Context(), userA, asked.ID)
	require.NoError(t, err)
	h.drain()
	assert.Equal(t, 4, h.evaluator.callCount())
	assert.Equal(t, models.EvaluationDone, loadResult(t, asked.ID).EvaluationStatus)
	assert.Equal(t, int64(2), count(t, &models.SearchResult{}, waiting, userA, models.EvaluationPending))

	// Still within the window: nothing more.
	h.clock.Advance(23 * time.Hour)
	h.drain()
	assert.Equal(t, 4, h.evaluator.callCount())

	// The window has moved on past the first evaluations.
	h.clock.Advance(time.Hour + time.Second)
	h.drain()
	assert.Equal(t, 6, h.evaluator.callCount())
	assert.Equal(t, int64(0), count(t, &models.SearchResult{}, waiting, userA, models.EvaluationPending))
}

// Concurrent workers never take a user past the cap.
func TestEvaluate_DailyCapUnderConcurrentWorkers(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"), withDailyCap(5))
	seedCV(t, user, time.Now())
	h.evaluator.delay = 5 * time.Millisecond
	for i := range 20 {
		seedEmbeddedJob(t, search, "linkedin", fmt.Sprint(i))
	}

	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			for {
				found, err := h.pipeline.EvaluateOne(t.Context())
				if !assert.NoError(t, err) || !found {
					return
				}
			}
		})
	}
	wg.Wait()

	assert.Equal(t, 5, h.evaluator.callCount())
	assert.Equal(t, int64(15), count(t, &models.SearchResult{}, "evaluation_status = ?", models.EvaluationPending))
}
