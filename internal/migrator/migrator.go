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

	if err := backfillCurrentEvaluations(db); err != nil {
		return err
	}

	slog.Info("migrated database")
	return nil
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
