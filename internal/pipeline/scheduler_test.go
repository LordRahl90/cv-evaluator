package pipeline

import (
	"errors"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/queue"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduler_FansOutPerProvider(t *testing.T) {
	due := newSearch(ulid.Make(), "go")
	disabled := newSearch(ulid.Make(), "off")
	disabled.Enabled = false
	future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	notDue := newSearch(ulid.Make(), "later")
	notDue.NextRunAt = &future

	h := newHarness(t, newFakeSearches(due, disabled, notDue), newFakeFetcher("linkedin", "jobindex"))

	assert.Equal(t, 2, h.tick())

	var tasks []models.QueueTask
	require.NoError(t, testDB.Order("unique_key").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	for i, provider := range []string{"jobindex", "linkedin"} {
		var p RunSearchPayload
		require.NoError(t, queue.Decode(&tasks[i], &p))
		assert.Equal(t, RunSearchPayload{SearchID: due.ID, Provider: provider}, p)
		assert.Equal(t, runSearchKey(due.ID, provider), *tasks[i].UniqueKey)
	}

	assert.Equal(t, 1, h.searches.marks[due.ID])
	assert.Zero(t, h.searches.marks[disabled.ID])
	assert.Zero(t, h.searches.marks[notDue.ID])
}

func TestScheduler_DedupesWhileQueued(t *testing.T) {
	search := newSearch(ulid.Make(), "go")
	searches := newFakeSearches(search)
	searches.markErr = errors.New("db down")
	h := newHarness(t, searches, newFakeFetcher("linkedin", "jobindex"))

	_, err := h.scheduler.Tick(t.Context())
	require.Error(t, err, "a failed MarkRun is reported")

	// The search is still due, but its tasks are already queued.
	searches.markErr = nil
	assert.Equal(t, 0, h.tick())
	assert.Equal(t, int64(2), count(t, &models.QueueTask{}))
	assert.Equal(t, 1, searches.marks[search.ID])
}

func TestScheduler_NoProviders(t *testing.T) {
	search := newSearch(ulid.Make(), "go")
	h := newHarness(t, newFakeSearches(search), newFakeFetcher())

	assert.Equal(t, 0, h.tick())
	assert.Zero(t, h.searches.marks[search.ID], "a search is not marked run when nothing ran")
}

func TestScheduler_DrainsInBatches(t *testing.T) {
	var searches []*Search
	for range 7 {
		searches = append(searches, newSearch(ulid.Make(), "q"))
	}
	resetDB(t)
	fs := newFakeSearches(searches...)
	q := queue.New(testDB, queue.Options{})
	s := NewScheduler(q, fs, newFakeFetcher("linkedin"), SchedulerOptions{BatchSize: 3})

	n, err := s.Tick(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 7, n)
}

func TestScheduler_NoSourcesIsQuiet(t *testing.T) {
	resetDB(t)
	q := queue.New(testDB, queue.Options{})
	s := NewScheduler(q, NoSources{}, NoSources{}, SchedulerOptions{})
	n, err := s.Tick(t.Context())
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
