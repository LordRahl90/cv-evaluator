package pipeline

import (
	"context"
	"fmt"
	"time"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/services/matcher"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LatestCVSelector selects the user's most recently uploaded CV.
//
// It stands in until users can mark an active CV (Phase 2); swap it for a
// selector over that without touching the pipeline.
type LatestCVSelector struct {
	db *gorm.DB
}

// NewLatestCVSelector returns a selector over db.
func NewLatestCVSelector(db *gorm.DB) *LatestCVSelector {
	return &LatestCVSelector{db: db}
}

// SelectCV returns the user's newest CV, or nil when they have none.
func (s *LatestCVSelector) SelectCV(ctx context.Context, userID ulid.ULID) (*models.CV, error) {
	var cvs []models.CV
	err := s.db.WithContext(ctx).
		Select("id", "created_at", "user_id").
		Where("user_id = ?", userID).
		Order("created_at DESC, id DESC").
		Limit(1).
		Find(&cvs).Error
	if err != nil {
		return nil, fmt.Errorf("select CV for user %s: %w", userID, err)
	}
	if len(cvs) == 0 {
		return nil, nil
	}
	return &cvs[0], nil
}

// Claiming is one short statement (its own transaction): pick a ready row
// with FOR UPDATE SKIP LOCKED, mark it processing and count the attempt,
// and return it. No transaction or connection is held while the slow work
// (scraping, embedding, the LLM) runs; the outcome is written afterwards.
//
// A row left in processing by a worker that died is claimed again once
// claimed_at is older than the stale timeout. There is no fencing: if a slow
// worker and a reclaimer both finish, both write, and since every write is
// an idempotent upsert (one job per (provider, job_id), one evaluation per
// (search result, CV), scores replaced as a set) the rare double run is
// harmless.

const claimJobSQL = `
UPDATE jobs SET status = @processing, claimed_at = @now, attempts = attempts + 1, updated_at = @now
WHERE id = (
	SELECT id FROM jobs
	WHERE deleted_at IS NULL AND (
		(status IN @ready AND (next_attempt_at IS NULL OR next_attempt_at <= @now))
		OR (status = @processing AND claimed_at < @stale)
	)
	ORDER BY next_attempt_at ASC NULLS FIRST, created_at ASC, id ASC
	LIMIT 1
	FOR UPDATE SKIP LOCKED
)
RETURNING *`

// claimJob claims one job that needs describing or embedding, or nil when
// none is ready.
func claimJob(db *gorm.DB, now, stale time.Time) (*models.Job, error) {
	var jobs []models.Job
	err := db.Raw(claimJobSQL, map[string]any{
		"processing": models.JobStatusProcessing,
		"ready":      []string{models.JobStatusNew, models.JobStatusDescribed},
		"now":        now,
		"stale":      stale,
	}).Scan(&jobs).Error
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}

const claimResultSQL = `
UPDATE search_results SET evaluation_status = @processing, claimed_at = @now, attempts = attempts + 1, updated_at = @now
WHERE id = (
	SELECT sr.id FROM search_results sr
	JOIN jobs j ON j.id = sr.job_id AND j.deleted_at IS NULL AND j.status = @embedded
	WHERE (sr.evaluation_status = @pending AND (sr.next_attempt_at IS NULL OR sr.next_attempt_at <= @now))
		OR (sr.evaluation_status = @processing AND sr.claimed_at < @stale)
	ORDER BY sr.next_attempt_at ASC NULLS FIRST, sr.created_at ASC, sr.id ASC
	LIMIT 1
	FOR UPDATE OF sr SKIP LOCKED
)
RETURNING *`

// claimResult claims one search result whose job is embedded and that
// waits for evaluation, or nil when none is ready.
func claimResult(db *gorm.DB, now, stale time.Time) (*models.SearchResult, error) {
	var results []models.SearchResult
	err := db.Raw(claimResultSQL, map[string]any{
		"processing": models.EvaluationProcessing,
		"pending":    models.EvaluationPending,
		"embedded":   models.JobStatusEmbedded,
		"now":        now,
		"stale":      stale,
	}).Scan(&results).Error
	if err != nil {
		return nil, fmt.Errorf("claim search result: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}
	return &results[0], nil
}

// upsertSearchResult records that search found job, once. A result that
// already exists keeps its state.
//
// A run takes a while and holds no lock, so the search (or its whole
// account) may be deleted before its results are stored. The insert first
// takes a share lock on the search row and is skipped when the row is
// gone or soft-deleted, so a late run never recreates rows for a deleted
// search or user. Account deletion locks the searches before deleting
// results (see services/account), so the two cannot interleave.
func upsertSearchResult(db *gorm.DB, search *Search, jobID ulid.ULID) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		var live []ulid.ULID
		if err := tx.Model(&models.JobSearch{}).
			Clauses(clause.Locking{Strength: "SHARE"}).
			Where("id = ?", search.ID).
			Pluck("id", &live).Error; err != nil {
			return err
		}
		if len(live) == 0 {
			return nil
		}
		result := &models.SearchResult{
			JobSearchID:      search.ID,
			UserID:           search.UserID,
			JobID:            jobID,
			Status:           models.SearchResultNew,
			EvaluationStatus: models.EvaluationPending,
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "job_search_id"}, {Name: "job_id"}},
			DoNothing: true,
		}).Create(result).Error
	})
	if err != nil {
		return fmt.Errorf("upsert search result: %w", err)
	}
	return nil
}

// lockSearchResult locks the search result's row for the rest of tx and
// reports whether it still exists. A worker's outcome is only written when
// it does: the result (or its account) may have been deleted while the
// worker evaluated it, and writing then would leave orphaned evaluations.
func lockSearchResult(tx *gorm.DB, id ulid.ULID) (bool, error) {
	var ids []ulid.ULID
	if err := tx.Model(&models.SearchResult{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).
		Pluck("id", &ids).Error; err != nil {
		return false, fmt.Errorf("lock search result: %w", err)
	}
	return len(ids) == 1, nil
}

// saveEvaluation inserts the evaluation for (search result, CV), or
// overwrites the one already there, and replaces its criterion scores.
// It must run inside the caller's transaction so the evaluation and its
// scores change together.
func saveEvaluation(tx *gorm.DB, e *models.Evaluation, breakdown []matcher.CriterionScore) error {
	err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "search_result_id"}, {Name: "cv_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"updated_at", "overall_score", "strengths", "missing_skills", "summary",
			"raw_response", "model", "prompt_version", "status", "error",
		}),
	}).Create(e).Error
	if err != nil {
		return fmt.Errorf("save evaluation: %w", err)
	}

	// On conflict the existing row keeps its id; read it back.
	var ids []ulid.ULID
	if err := tx.Model(&models.Evaluation{}).
		Where("search_result_id = ? AND cv_id = ?", e.SearchResultID, e.CVID).
		Pluck("id", &ids).Error; err != nil {
		return fmt.Errorf("load evaluation id: %w", err)
	}
	if len(ids) != 1 {
		return fmt.Errorf("load evaluation id: found %d rows", len(ids))
	}
	id := ids[0]
	e.ID = id

	if err := tx.Where("evaluation_id = ?", id).Delete(&models.EvaluationScore{}).Error; err != nil {
		return fmt.Errorf("clear evaluation scores: %w", err)
	}
	if len(breakdown) == 0 {
		return nil
	}
	scores := make([]models.EvaluationScore, len(breakdown))
	for i, b := range breakdown {
		scores[i] = models.EvaluationScore{EvaluationID: id, Criterion: b.Criterion, Score: b.Score, Reason: b.Reason}
	}
	if err := tx.Create(&scores).Error; err != nil {
		return fmt.Errorf("save evaluation scores: %w", err)
	}
	return nil
}
