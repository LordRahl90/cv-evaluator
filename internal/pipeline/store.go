package pipeline

import (
	"context"
	"fmt"

	"cv-evaluator/internal/models"

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

// store holds the pipeline's own persistence: search results and
// evaluations.
type store struct {
	db *gorm.DB
}

// upsertSearchResult records that search found job, once. It returns the
// stored result, whether or not this call created it.
func (s *store) upsertSearchResult(ctx context.Context, search *Search, jobID ulid.ULID) (*models.SearchResult, error) {
	result := &models.SearchResult{
		JobSearchID: search.ID,
		UserID:      search.UserID,
		JobID:       jobID,
		Status:      models.SearchResultNew,
	}
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "job_search_id"}, {Name: "job_id"}},
			DoNothing: true,
		}).
		Create(result).Error
	if err != nil {
		return nil, fmt.Errorf("upsert search result: %w", err)
	}

	var stored models.SearchResult
	if err := s.db.WithContext(ctx).
		Where("job_search_id = ? AND job_id = ?", search.ID, jobID).
		Take(&stored).Error; err != nil {
		return nil, fmt.Errorf("load search result: %w", err)
	}
	return &stored, nil
}

func (s *store) searchResult(ctx context.Context, id ulid.ULID) (*models.SearchResult, error) {
	var result models.SearchResult
	if err := s.db.WithContext(ctx).Where("id = ?", id).Take(&result).Error; err != nil {
		return nil, err
	}
	return &result, nil
}

// unevaluatedResults lists the search results for job that have no
// completed evaluation yet.
func (s *store) unevaluatedResults(ctx context.Context, jobID ulid.ULID) ([]models.SearchResult, error) {
	var results []models.SearchResult
	err := s.db.WithContext(ctx).
		Where("job_id = ?", jobID).
		Where("NOT EXISTS (SELECT 1 FROM evaluations e WHERE e.search_result_id = search_results.id AND e.status = ?)",
			models.StatusCompleted).
		Order("id").
		Find(&results).Error
	if err != nil {
		return nil, fmt.Errorf("list unevaluated results for job %s: %w", jobID, err)
	}
	return results, nil
}

func (s *store) evaluation(ctx context.Context, resultID, cvID ulid.ULID) (*models.Evaluation, error) {
	var evals []models.Evaluation
	err := s.db.WithContext(ctx).
		Where("search_result_id = ? AND cv_id = ?", resultID, cvID).
		Limit(1).
		Find(&evals).Error
	if err != nil {
		return nil, err
	}
	if len(evals) == 0 {
		return nil, nil
	}
	return &evals[0], nil
}

// saveEvaluation inserts e, or overwrites the evaluation already stored for
// the same (search result, CV).
func (s *store) saveEvaluation(ctx context.Context, e *models.Evaluation) error {
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "search_result_id"}, {Name: "cv_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"updated_at", "overall_score", "skills_score", "experience_score", "seniority_score",
				"domain_score", "location_score", "strengths", "missing_skills", "summary",
				"raw_response", "model", "prompt_version", "status", "error",
			}),
		}).
		Create(e).Error
	if err != nil {
		return fmt.Errorf("save evaluation: %w", err)
	}
	return nil
}
