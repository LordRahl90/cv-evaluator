package migrator

import (
	"fmt"
	"log/slog"

	"cv-evaluator/internal/models"

	"gorm.io/gorm"
)

var appModels = []interface{}{
	&models.User{},
	&models.CV{},
	&models.Job{},
	&models.SectionEmbedding{},
	&models.APIKey{},
	&models.JobSearch{},
	&models.PortalSuggestion{},
	&models.SearchResult{},
	&models.Evaluation{},
	&models.EvaluationScore{},
}

func Migrate(db *gorm.DB) error {
	slog.Info("migrating database")

	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		slog.Warn("could not create vector extension", "error", err)
	}

	for _, model := range appModels {
		slog.Info(fmt.Sprintf("migrating model %T", model))
		if err := db.AutoMigrate(model); err != nil {
			slog.Error("failed to migrate model", "model", fmt.Sprintf("%T", model), "error", err)
			return err
		}
	}

	for _, stmt := range uniqueIndexes {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("create unique index: %w", err)
		}
	}

	for _, stmt := range indexes {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	if err := backfillCurrentEvaluations(db); err != nil {
		return err
	}
	if err := backfillEvaluatedAt(db); err != nil {
		return err
	}

	slog.Info("migrated database")
	return nil
}

// uniqueIndexes stop two accounts sharing an email (in any case) or a
// phone number. The services check first for a friendly error; these make
// concurrent sign-ups and email changes safe too.
var uniqueIndexes = []string{
	`CREATE UNIQUE INDEX IF NOT EXISTS ` + models.UserEmailUniqueIndex + ` ON users (lower(email)) WHERE deleted_at IS NULL`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ` + models.UserPhoneUniqueIndex + ` ON users (phone) WHERE phone <> '' AND deleted_at IS NULL`,
}

// indexes are the ones GORM tags cannot express.
//
// idx_search_results_claim serves the worker's claim (see
// pipeline.claimCandidateSQL): it walks results waiting for evaluation in
// claim order and stops at the first one it may take.
var indexes = []string{
	`CREATE INDEX IF NOT EXISTS idx_search_results_claim ON search_results
		(forced DESC, similarity DESC NULLS LAST, next_attempt_at ASC NULLS FIRST, created_at ASC, id ASC)
		WHERE evaluation_status IN ('pending', 'processing')`,
}

// backfillCurrentEvaluationsSQL points each evaluated search result that has
// no current evaluation yet at its most recently written one, and copies its
// score when it completed. Results evaluated before search_results had these
// columns need it; for every other row it is a no-op.
const backfillCurrentEvaluationsSQL = `
UPDATE search_results sr
SET evaluation_id = e.id,
	overall_score = CASE WHEN e.status = @completed THEN e.overall_score END
FROM (
	SELECT DISTINCT ON (search_result_id) id, search_result_id, status, overall_score
	FROM evaluations
	WHERE search_result_id IN (SELECT id FROM search_results WHERE evaluation_id IS NULL)
	ORDER BY search_result_id, updated_at DESC, id DESC
) e
WHERE e.search_result_id = sr.id AND sr.evaluation_id IS NULL`

func backfillCurrentEvaluations(db *gorm.DB) error {
	res := db.Exec(backfillCurrentEvaluationsSQL, map[string]any{"completed": models.StatusCompleted})
	if res.Error != nil {
		return fmt.Errorf("backfill current evaluations: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		slog.Info("backfilled current evaluations", "search_results", res.RowsAffected)
	}
	return nil
}

// backfillEvaluatedAtSQL sets when each evaluated search result was last
// sent to the LLM, from its current evaluation, for results evaluated
// before search_results had the column. The worker's daily cap counts it.
const backfillEvaluatedAtSQL = `
UPDATE search_results sr
SET evaluated_at = e.updated_at
FROM evaluations e
WHERE e.id = sr.evaluation_id AND sr.evaluated_at IS NULL`

func backfillEvaluatedAt(db *gorm.DB) error {
	res := db.Exec(backfillEvaluatedAtSQL)
	if res.Error != nil {
		return fmt.Errorf("backfill evaluated_at: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		slog.Info("backfilled evaluated_at", "search_results", res.RowsAffected)
	}
	return nil
}
