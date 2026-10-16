package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cv-evaluator/internal/entities"
	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTick_RunsDueSearchesOnEveryPortal(t *testing.T) {
	due := newSearch(ulid.Make(), "go")
	disabled := newSearch(ulid.Make(), "off")
	disabled.Enabled = false
	future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	notDue := newSearch(ulid.Make(), "later")
	notDue.NextRunAt = &future
	unscheduled := newSearch(ulid.Make(), "unscheduled")
	unscheduled.NextRunAt = nil

	fetcher := newFakeFetcher("linkedin", "jobindex")
	h := newHarness(t, newFakeSearches(due, disabled, notDue, unscheduled), fetcher)

	assert.Equal(t, 1, h.tick())
	assert.Equal(t, 1, fetcher.searchCount("linkedin"))
	assert.Equal(t, 1, fetcher.searchCount("jobindex"))
	assert.Equal(t, []Criteria{
		{Query: "go", Location: "Copenhagen", MaxPages: DefaultMaxPages},
		{Query: "go", Location: "Copenhagen", MaxPages: DefaultMaxPages},
	}, fetcher.criteria)

	assert.Equal(t, 1, h.searches.markCount(due.ID))
	for _, s := range []*Search{disabled, notDue, unscheduled} {
		assert.Zero(t, h.searches.claimCount(s.ID))
	}
	got := h.searches.get(due.ID)
	assert.Equal(t, h.clock.Now().Add(24*time.Hour), *got.NextRunAt)
}

func TestTick_StoresResultsOnceAcrossReruns(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.setPosts("linkedin", post("linkedin", "1", "d1"), post("linkedin", "2", ""), post("linkedin", "", "no id"))
	h := newHarness(t, newFakeSearches(search), fetcher)

	for range 3 {
		h.tick()
		h.clock.Advance(25 * time.Hour)
	}

	assert.Equal(t, 3, fetcher.searchCount("linkedin"))
	assert.Equal(t, int64(2), count(t, &models.Job{}), "postings without an id are skipped")
	assert.Equal(t, int64(1), count(t, &models.Job{}, "status = ?", models.JobStatusDescribed))
	assert.Equal(t, int64(1), count(t, &models.Job{}, "status = ?", models.JobStatusNew))
	assert.Equal(t, int64(2), count(t, &models.SearchResult{}))

	var r models.SearchResult
	require.NoError(t, testDB.First(&r).Error)
	assert.Equal(t, models.SearchResultNew, r.Status)
	assert.Equal(t, models.EvaluationPending, r.EvaluationStatus)
	assert.Equal(t, user, r.UserID)
}

// Two worker processes ticking at the same time run each search once.
func TestTick_ConcurrentSchedulersRunEachSearchOnce(t *testing.T) {
	var searches []*Search
	for range 6 {
		searches = append(searches, newSearch(ulid.Make(), "q"))
	}
	store := newFakeSearches(searches...)
	fetcher := newFakeFetcher("linkedin", "jobindex")
	h := newHarness(t, store, fetcher)
	other, err := New(h.pipeline.cfg)
	require.NoError(t, err)

	var (
		wg  sync.WaitGroup
		ran [2]int
	)
	for i, p := range []*Pipeline{h.pipeline, other} {
		wg.Go(func() {
			n, err := p.Tick(t.Context())
			assert.NoError(t, err)
			ran[i] = n
		})
	}
	wg.Wait()

	assert.Equal(t, 6, ran[0]+ran[1])
	for _, s := range searches {
		assert.Equal(t, 1, store.claimCount(s.ID))
		assert.Equal(t, 1, store.markCount(s.ID))
	}
	assert.Equal(t, 6, fetcher.searchCount("linkedin"))
	assert.Equal(t, 6, fetcher.searchCount("jobindex"))
}

func TestTick_PortalFailureDoesNotAffectOthers(t *testing.T) {
	search := newSearch(ulid.Make(), "go")
	fetcher := newFakeFetcher("linkedin", "jobindex")
	fetcher.setPosts("linkedin", post("linkedin", "1", "d"))
	fetcher.searchErr["jobindex"] = errors.New("jobindex is down")
	h := newHarness(t, newFakeSearches(search), fetcher)

	assert.Equal(t, 1, h.tick())

	assert.Equal(t, DefaultPortalAttempts, fetcher.searchCount("jobindex"), "a failing portal is retried a few times")
	assert.Equal(t, 1, fetcher.searchCount("linkedin"))
	assert.Equal(t, int64(1), count(t, &models.SearchResult{}))
	assert.Equal(t, 1, h.searches.markCount(search.ID), "the search is still marked run")
}

func TestTick_MarkRunFailureLeavesLease(t *testing.T) {
	search := newSearch(ulid.Make(), "go")
	store := newFakeSearches(search)
	store.markErr = errors.New("db down")
	h := newHarness(t, store, newFakeFetcher("linkedin"))

	_, err := h.pipeline.Tick(t.Context())
	require.Error(t, err)

	// The claim's lease keeps the search from running again right away...
	store.markErr = nil
	assert.Equal(t, 0, h.tick())
	// ...and once it expires the search is due again.
	h.clock.Advance(DefaultRunTimeout + time.Second)
	assert.Equal(t, 1, h.tick())
}

type blockingFetcher struct {
	*fakeFetcher
	started chan struct{}
	once    sync.Once
}

func (f *blockingFetcher) Search(ctx context.Context, _ string, _ Criteria) ([]entities.JobPost, error) {
	f.once.Do(func() { close(f.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestTick_ShutdownMidRunDoesNotMarkRun(t *testing.T) {
	search := newSearch(ulid.Make(), "go")
	store := newFakeSearches(search)
	fetcher := &blockingFetcher{fakeFetcher: newFakeFetcher("linkedin"), started: make(chan struct{})}
	h := newHarness(t, store, fetcher.fakeFetcher, func(c *Config) { c.Fetcher = fetcher })

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := h.pipeline.Tick(ctx)
		done <- err
	}()
	<-fetcher.started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	assert.Equal(t, 1, store.claimCount(search.ID))
	assert.Zero(t, store.markCount(search.ID), "an interrupted run is not marked; its lease expires")
}

func TestTick_NoProviders(t *testing.T) {
	search := newSearch(ulid.Make(), "go")
	h := newHarness(t, newFakeSearches(search), newFakeFetcher())

	assert.Equal(t, 0, h.tick())
	assert.Zero(t, h.searches.claimCount(search.ID))
}

func TestTick_NoSourcesIsQuiet(t *testing.T) {
	resetDB(t)
	p, err := New(Config{DB: testDB, Searches: NoSources{}, Fetcher: NoSources{}, Embedder: &fakeEmbedder{}, Evaluator: &fakeEvaluator{}})
	require.NoError(t, err)
	n, err := p.Tick(t.Context())
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestNextRunAfter(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, from.Add(time.Hour), NextRunAfter(FrequencyHourly, from))
	assert.Equal(t, from.Add(24*time.Hour), NextRunAfter(FrequencyDaily, from))
	assert.Equal(t, from.Add(7*24*time.Hour), NextRunAfter(FrequencyWeekly, from))
	assert.Equal(t, from.Add(24*time.Hour), NextRunAfter("fortnightly", from))
}
