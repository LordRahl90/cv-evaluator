package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Search result statuses. A result starts as new; the user moves it on.
const (
	SearchResultNew       = "new"
	SearchResultSaved     = "saved"
	SearchResultApplied   = "applied"
	SearchResultDismissed = "dismissed"
)

// Search result evaluation statuses. A result is pending until it has been
// evaluated against the user's CV, then done; it is processing while a
// worker evaluates it (ClaimedAt says since when), and failed once
// evaluation has failed MaxAttempts times. Setting it back to pending (with
// Attempts 0 and NextAttemptAt nil) re-evaluates it.
const (
	EvaluationPending    = "pending"
	EvaluationProcessing = "processing"
	EvaluationDone       = "done"
	EvaluationFailed     = "failed"
)

// SearchResult links a global Job to the saved search (and so the user)
// that found it. A search lists each job at most once.
//
// JobSearchID refers to a JobSearch, but there is deliberately no GORM
// association to it so this model does not depend on that one.
//
// EvaluationID is the result's current evaluation: the one the pipeline
// wrote last, nil until the result is first evaluated. OverallScore copies
// that evaluation's overall score while it is completed (nil otherwise), so
// results can be filtered and sorted by score straight from this table.
//
// The idx_search_results_user_* and idx_search_results_search_score indexes
// serve the results API: a user's (or one search's) results by best score
// with unscored ones last, newest first, and counts by status. The trailing
// id makes the order total for cursor pagination.
type SearchResult struct {
	ID               ulid.ULID `gorm:"type:bytea;primaryKey;index:idx_search_results_user_score,priority:3,sort:desc;index:idx_search_results_search_score,priority:3,sort:desc;index:idx_search_results_user_created,priority:3,sort:desc"`
	CreatedAt        time.Time `gorm:"index:idx_search_results_user_created,priority:2,sort:desc"`
	UpdatedAt        time.Time
	JobSearchID      ulid.ULID  `gorm:"type:bytea;not null;index;uniqueIndex:idx_search_results_search_job,priority:1;index:idx_search_results_search_score,priority:1"`
	UserID           ulid.ULID  `gorm:"type:bytea;not null;index;index:idx_search_results_user_score,priority:1;index:idx_search_results_user_created,priority:1;index:idx_search_results_user_status,priority:1"`
	JobID            ulid.ULID  `gorm:"type:bytea;not null;index;uniqueIndex:idx_search_results_search_job,priority:2"`
	Status           string     `gorm:"not null;default:new;index:idx_search_results_user_status,priority:2"`
	EvaluationStatus string     `gorm:"not null;default:pending;index:idx_search_results_work,priority:1"`
	Attempts         int        `gorm:"not null;default:0"`
	NextAttemptAt    *time.Time `gorm:"index:idx_search_results_work,priority:2"`
	ClaimedAt        *time.Time
	LastError        string     `gorm:"type:text"`
	EvaluationID     *ulid.ULID `gorm:"type:bytea"`
	OverallScore     *int       `gorm:"index:idx_search_results_user_score,priority:2,sort:desc nulls last;index:idx_search_results_search_score,priority:2,sort:desc nulls last"`
}

// SearchResultStatuses lists every valid SearchResult.Status value.
var SearchResultStatuses = []string{SearchResultNew, SearchResultSaved, SearchResultApplied, SearchResultDismissed}

// EvaluationStatuses lists every valid SearchResult.EvaluationStatus value.
var EvaluationStatuses = []string{EvaluationPending, EvaluationProcessing, EvaluationDone, EvaluationFailed}

func (r *SearchResult) BeforeCreate(*gorm.DB) error {
	if r.ID == (ulid.ULID{}) {
		r.ID = ulid.Make()
	}
	if r.Status == "" {
		r.Status = SearchResultNew
	}
	if r.EvaluationStatus == "" {
		r.EvaluationStatus = EvaluationPending
	}

	return nil
}
