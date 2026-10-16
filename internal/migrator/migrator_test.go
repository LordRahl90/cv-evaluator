package migrator_test

import (
	"testing"

	"cv-evaluator/internal/migrator"
	"cv-evaluator/internal/models"
	"cv-evaluator/internal/testutil/postgres"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrate_ResultIndexesAndBackfill(t *testing.T) {
	container := postgres.MustRun(t)
	db, err := container.OpenGorm(nil)
	require.NoError(t, err)

	var defs []string
	require.NoError(t, db.Raw(`SELECT indexdef FROM pg_indexes WHERE tablename = 'search_results'
		AND indexname IN ('idx_search_results_user_score', 'idx_search_results_search_score',
			'idx_search_results_user_created', 'idx_search_results_user_status')
		ORDER BY indexname`).Scan(&defs).Error)
	require.Len(t, defs, 4)
	assert.Contains(t, defs[0], "(job_search_id, overall_score DESC NULLS LAST, id DESC)")
	assert.Contains(t, defs[1], "(user_id, created_at DESC, id DESC)")
	assert.Contains(t, defs[2], "(user_id, overall_score DESC NULLS LAST, id DESC)")
	assert.Contains(t, defs[3], "(user_id, status)")

	// Results evaluated before the current-evaluation columns existed.
	scored := &models.SearchResult{JobSearchID: ulid.Make(), UserID: ulid.Make(), JobID: ulid.Make(), EvaluationStatus: models.EvaluationDone}
	failed := &models.SearchResult{JobSearchID: ulid.Make(), UserID: ulid.Make(), JobID: ulid.Make(), EvaluationStatus: models.EvaluationFailed}
	unevaluated := &models.SearchResult{JobSearchID: ulid.Make(), UserID: ulid.Make(), JobID: ulid.Make()}
	require.NoError(t, db.Create([]*models.SearchResult{scored, failed, unevaluated}).Error)
	good := &models.Evaluation{SearchResultID: scored.ID, CVID: ulid.Make(), OverallScore: 77, Status: models.StatusCompleted}
	bad := &models.Evaluation{SearchResultID: failed.ID, CVID: ulid.Make(), OverallScore: 0, Status: models.StatusFailed}
	require.NoError(t, db.Create([]*models.Evaluation{good, bad}).Error)

	require.NoError(t, migrator.Migrate(db))

	var got []models.SearchResult
	require.NoError(t, db.Order("id").Find(&got, "id IN ?", []ulid.ULID{scored.ID, failed.ID, unevaluated.ID}).Error)
	byID := map[ulid.ULID]models.SearchResult{}
	for _, r := range got {
		byID[r.ID] = r
	}
	require.NotNil(t, byID[scored.ID].EvaluationID)
	assert.Equal(t, good.ID, *byID[scored.ID].EvaluationID)
	require.NotNil(t, byID[scored.ID].OverallScore)
	assert.Equal(t, 77, *byID[scored.ID].OverallScore)
	require.NotNil(t, byID[failed.ID].EvaluationID)
	assert.Nil(t, byID[failed.ID].OverallScore, "a failed evaluation has no score")
	assert.Nil(t, byID[unevaluated.ID].EvaluationID)

	require.NoError(t, migrator.Migrate(db), "migrating again is a no-op")
}
