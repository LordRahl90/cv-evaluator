package pipeline

import (
	"errors"
	"sync"
	"testing"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/account"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// seedUser stores the users row that account deletion needs.
func seedUser(t *testing.T, id ulid.ULID) {
	t.Helper()
	require.NoError(t, testDB.Create(&models.User{ID: id, Email: id.String() + "@example.com"}).Error)
}

func deleteAccount(t *testing.T, userID ulid.ULID) {
	t.Helper()
	_, err := account.New(testDB).Delete(t.Context(), userID)
	require.NoError(t, err)
}

// A worker that claimed a result and is still waiting on the LLM when the
// user deletes their account must not write an evaluation (or anything
// else) for it afterwards, whether the evaluation succeeded or failed.
func TestEvaluateOne_AfterAccountDeletion_WritesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"evaluation succeeds", nil},
		{"evaluation fails", errors.New("LLM unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := ulid.Make()
			search := newSearch(user, "go")
			h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
			seedUser(t, user)
			seedCV(t, user, time.Now())
			job, _ := seedEmbeddedJob(t, search, "linkedin", "1")

			h.evaluator.fail = func(int) (string, error) {
				deleteAccount(t, user) // while the worker is in the LLM call
				return "raw", tc.err
			}

			found, err := h.pipeline.EvaluateOne(t.Context())
			require.NoError(t, err, "a vanished result is not an error")
			assert.True(t, found)
			assert.Equal(t, 1, h.evaluator.callCount())

			assert.Zero(t, count(t, &models.Evaluation{}), "no orphaned evaluation")
			assert.Zero(t, count(t, &models.EvaluationScore{}))
			assert.Zero(t, count(t, &models.SearchResult{}), "the result is not recreated")
			assert.Equal(t, int64(1), count(t, &models.Job{}, "id = ?", job.ID), "the global job stays")

			found, err = h.pipeline.EvaluateOne(t.Context())
			require.NoError(t, err)
			assert.False(t, found, "nothing left to evaluate")
		})
	}
}

// A search run that started before the account was deleted (the scheduler
// already loaded the search) stores the global jobs it found but no
// search results for the deleted search.
func TestRunSearch_AfterAccountDeletion_StoresNoResults(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	fetcher := newFakeFetcher("linkedin")
	fetcher.setPosts("linkedin", post("linkedin", "L1", "inline"), post("linkedin", "L2", "inline"))
	h := newHarness(t, newFakeSearches(search), fetcher)
	seedUser(t, user)
	seedCV(t, user, time.Now())

	deleteAccount(t, user)

	assert.Equal(t, 1, h.tick(), "the stale scheduler still runs the search")
	h.drain()

	assert.Equal(t, int64(2), count(t, &models.Job{}), "postings are global and still stored")
	assert.Zero(t, count(t, &models.SearchResult{}), "no results for a deleted search")
	assert.Zero(t, count(t, &models.Evaluation{}))
	assert.Zero(t, h.evaluator.callCount())
}

// A soft-deleted search gets no new results either.
func TestUpsertSearchResult_SkipsDeletedSearch(t *testing.T) {
	resetDB(t)
	search := newSearch(ulid.Make(), "go")
	job := &models.Job{Provider: "linkedin", JobID: ulid.Make().String()}
	require.NoError(t, testDB.Create(job).Error)
	require.NoError(t, testDB.Delete(&models.JobSearch{ID: search.ID}).Error)

	require.NoError(t, upsertSearchResult(testDB, search, job.ID))
	assert.Zero(t, count(t, &models.SearchResult{}))
}

// The insert waits for a deletion that holds the search's lock and then
// finds the search gone, instead of slipping a result in between the
// deletion's statements.
func TestUpsertSearchResult_WaitsForConcurrentDeletion(t *testing.T) {
	resetDB(t)
	search := newSearch(ulid.Make(), "go")
	job := &models.Job{Provider: "linkedin", JobID: ulid.Make().String()}
	require.NoError(t, testDB.Create(job).Error)

	tx := testDB.Begin()
	require.NoError(t, tx.Error)
	var ids []ulid.ULID
	require.NoError(t, tx.Model(&models.JobSearch{}).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", search.ID).Pluck("id", &ids).Error)
	require.Len(t, ids, 1)

	var (
		wg        sync.WaitGroup
		upsertErr error
		done      = make(chan struct{})
	)
	wg.Go(func() {
		defer close(done)
		upsertErr = upsertSearchResult(testDB.WithContext(t.Context()), search, job.ID)
	})

	select {
	case <-done:
		t.Fatal("the insert did not wait for the deletion's lock")
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, tx.Session(&gorm.Session{NewDB: true}).Unscoped().
		Where("id = ?", search.ID).Delete(&models.JobSearch{}).Error)
	require.NoError(t, tx.Commit().Error)
	wg.Wait()

	require.NoError(t, upsertErr)
	assert.Zero(t, count(t, &models.SearchResult{}))
}
