package pipeline

import (
	"math"
	"testing"
	"time"

	"cv-evaluator/internal/models"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unit returns a 768-dimension vector with the given leading values.
func unit(values ...float32) []float32 {
	v := make([]float32, 768)
	copy(v, values)
	return v
}

// seedJobEmbedding sets the job's embedding; nil stores NULL.
func seedJobEmbedding(t *testing.T, jobID ulid.ULID, v []float32) {
	t.Helper()
	var value any
	if v != nil {
		value = pgvector.NewVector(v)
	}
	require.NoError(t, testDB.Model(&models.Job{}).Where("id = ?", jobID).Update("embedding", value).Error)
}

// seedCVEmbedding sets the CV's embedding; nil stores NULL.
func seedCVEmbedding(t *testing.T, cvID ulid.ULID, v []float32) {
	t.Helper()
	var value any
	if v != nil {
		value = pgvector.NewVector(v)
	}
	require.NoError(t, testDB.Model(&models.CV{}).Where("id = ?", cvID).Update("full_embedding", value).Error)
}

func TestEvaluateOne_RecordsSimilarity(t *testing.T) {
	user := ulid.Make()
	search := newSearch(user, "go")
	h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
	cv := seedCV(t, user, time.Now())
	seedCVEmbedding(t, cv.ID, unit(1, 1))
	job, result := seedEmbeddedJob(t, search, "linkedin", "1")
	seedJobEmbedding(t, job.ID, unit(1))

	found, err := h.pipeline.EvaluateOne(t.Context())
	require.NoError(t, err)
	require.True(t, found)

	var got models.SearchResult
	require.NoError(t, testDB.Take(&got, "id = ?", result.ID).Error)
	assert.Equal(t, models.EvaluationDone, got.EvaluationStatus)
	require.NotNil(t, got.Similarity)
	assert.InDelta(t, 1/math.Sqrt2, *got.Similarity, 1e-6)
	require.NotNil(t, got.SimilarityCVID)
	assert.Equal(t, cv.ID, *got.SimilarityCVID)

	var eval models.Evaluation
	require.NoError(t, testDB.Take(&eval, "search_result_id = ?", result.ID).Error)
	require.NotNil(t, eval.Similarity)
	assert.InDelta(t, 1/math.Sqrt2, *eval.Similarity, 1e-6)
}

// A missing or zero embedding gives no similarity, and the result is
// evaluated all the same.
func TestEvaluateOne_SimilarityMissingStillEvaluates(t *testing.T) {
	cases := []struct {
		name    string
		cv, job []float32 // nil stores NULL
	}{
		{name: "zero cv vector", cv: unit(), job: unit(1)},
		{name: "zero job vector", cv: unit(1), job: unit()},
		{name: "no cv embedding", job: unit(1)},
		{name: "no job embedding", cv: unit(1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := ulid.Make()
			search := newSearch(user, "go")
			h := newHarness(t, newFakeSearches(), newFakeFetcher("linkedin"))
			cv := seedCV(t, user, time.Now())
			seedCVEmbedding(t, cv.ID, tc.cv)
			job, result := seedEmbeddedJob(t, search, "linkedin", "1")
			seedJobEmbedding(t, job.ID, tc.job)

			found, err := h.pipeline.EvaluateOne(t.Context())
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, 1, h.evaluator.callCount())

			var got models.SearchResult
			require.NoError(t, testDB.Take(&got, "id = ?", result.ID).Error)
			assert.Equal(t, models.EvaluationDone, got.EvaluationStatus)
			assert.Nil(t, got.Similarity)
			require.NotNil(t, got.SimilarityCVID, "the CV it was tried against is recorded")
			assert.Equal(t, cv.ID, *got.SimilarityCVID)

			var eval models.Evaluation
			require.NoError(t, testDB.Take(&eval, "search_result_id = ?", result.ID).Error)
			assert.Nil(t, eval.Similarity)
			assert.Equal(t, models.StatusCompleted, eval.Status)
		})
	}
}
