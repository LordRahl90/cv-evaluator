// Package account works on everything a user owns at once: it exports it
// and it deletes it.
//
// A user owns rows in users, cvs, section_embeddings, job_searches,
// search_results, evaluations, evaluation_scores, api_keys and
// portal_suggestions. Jobs are global (shared by every user whose searches
// found them) and are never touched.
package account

import (
	"context"
	"errors"
	"fmt"

	"cv-evaluator/internal/models"
	"cv-evaluator/internal/telemetry"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrUserNotFound means there is no such user (it may already have been
// deleted).
var ErrUserNotFound = errors.New("user not found")

// Service exports and deletes accounts.
type Service struct {
	db *gorm.DB
}

// New returns a service over db.
func New(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Deleted is how many rows were deleted from one table.
type Deleted struct {
	Table string
	Rows  int64
}

// Delete permanently deletes the user and every row they own, soft-deleted
// rows included, in one transaction, and reports how many rows went from
// each table. It returns ErrUserNotFound when there is no such user.
//
// Children go before their parents (scores, evaluations, results, searches,
// sections, CVs, keys, suggestions, then the user), so the order is safe
// should foreign keys be added later.
//
// Background workers write without holding a transaction during their slow
// work and then write their outcome guarded by a lock on the row they
// claimed (the job search for new results, the search result for an
// evaluation). Delete locks the user's job searches and search results
// before deleting anything: a worker that got its lock first finishes and
// its rows are then seen and deleted here; one that comes later finds the
// row gone and writes nothing.
func (s *Service) Delete(ctx context.Context, userID ulid.ULID) (_ []Deleted, err error) {
	ctx, end := telemetry.Start(ctx, "account.Service.Delete", telemetry.ID(telemetry.AttrUserID, userID))
	defer end(&err)
	var report []Deleted
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		report = nil
		lock := clause.Locking{Strength: "UPDATE"}

		var users []ulid.ULID
		if err := tx.Unscoped().Model(&models.User{}).Clauses(lock).
			Where("id = ?", userID).Pluck("id", &users).Error; err != nil {
			return fmt.Errorf("lock user: %w", err)
		}
		if len(users) == 0 {
			return ErrUserNotFound
		}

		var ids []ulid.ULID
		if err := tx.Unscoped().Model(&models.JobSearch{}).Clauses(lock).
			Where("user_id = ?", userID).Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("lock job searches: %w", err)
		}
		if err := tx.Model(&models.SearchResult{}).Clauses(lock).
			Where("user_id = ? OR job_search_id IN (?)", userID, ownedSearches(tx, userID)).
			Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("lock search results: %w", err)
		}

		steps := []struct {
			table string
			model any
			where func() (string, []any)
		}{
			{"evaluation_scores", &models.EvaluationScore{}, func() (string, []any) {
				return "evaluation_id IN (?)", []any{ownedEvaluations(tx, userID)}
			}},
			{"evaluations", &models.Evaluation{}, func() (string, []any) {
				return "search_result_id IN (?) OR cv_id IN (?)", []any{ownedResults(tx, userID), ownedCVs(tx, userID)}
			}},
			{"search_results", &models.SearchResult{}, func() (string, []any) {
				return "user_id = ? OR job_search_id IN (?)", []any{userID, ownedSearches(tx, userID)}
			}},
			{"job_searches", &models.JobSearch{}, func() (string, []any) {
				return "user_id = ?", []any{userID}
			}},
			{"section_embeddings", &models.SectionEmbedding{}, func() (string, []any) {
				return "user_id = ? OR cv_id IN (?)", []any{userID, ownedCVs(tx, userID)}
			}},
			{"cvs", &models.CV{}, func() (string, []any) {
				return "user_id = ?", []any{userID}
			}},
			{"api_keys", &models.APIKey{}, func() (string, []any) {
				return "user_id = ?", []any{userID}
			}},
			{"portal_suggestions", &models.PortalSuggestion{}, func() (string, []any) {
				return "user_id = ?", []any{userID}
			}},
			{"users", &models.User{}, func() (string, []any) {
				return "id = ?", []any{userID}
			}},
		}
		for _, step := range steps {
			query, args := step.where()
			res := tx.Unscoped().Where(query, args...).Delete(step.model)
			if res.Error != nil {
				return fmt.Errorf("delete %s: %w", step.table, res.Error)
			}
			report = append(report, Deleted{Table: step.table, Rows: res.RowsAffected})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

// The subqueries below select the ids of rows the user owns, soft-deleted
// ones included. Each call builds a fresh query.

func ownedCVs(tx *gorm.DB, userID ulid.ULID) *gorm.DB {
	return tx.Session(&gorm.Session{NewDB: true}).Unscoped().
		Model(&models.CV{}).Select("id").Where("user_id = ?", userID)
}

func ownedSearches(tx *gorm.DB, userID ulid.ULID) *gorm.DB {
	return tx.Session(&gorm.Session{NewDB: true}).Unscoped().
		Model(&models.JobSearch{}).Select("id").Where("user_id = ?", userID)
}

func ownedResults(tx *gorm.DB, userID ulid.ULID) *gorm.DB {
	return tx.Session(&gorm.Session{NewDB: true}).
		Model(&models.SearchResult{}).Select("id").
		Where("user_id = ? OR job_search_id IN (?)", userID, ownedSearches(tx, userID))
}

func ownedEvaluations(tx *gorm.DB, userID ulid.ULID) *gorm.DB {
	return tx.Session(&gorm.Session{NewDB: true}).
		Model(&models.Evaluation{}).Select("id").
		Where("search_result_id IN (?) OR cv_id IN (?)", ownedResults(tx, userID), ownedCVs(tx, userID))
}
