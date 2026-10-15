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

// SearchResult links a global Job to the saved search (and so the user)
// that found it. A search lists each job at most once.
//
// JobSearchID refers to a JobSearch, but there is deliberately no GORM
// association to it so this model does not depend on that one.
type SearchResult struct {
	ID          ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	JobSearchID ulid.ULID `gorm:"type:bytea;not null;index;uniqueIndex:idx_search_results_search_job,priority:1"`
	UserID      ulid.ULID `gorm:"type:bytea;not null;index"`
	JobID       ulid.ULID `gorm:"type:bytea;not null;index;uniqueIndex:idx_search_results_search_job,priority:2"`
	Status      string    `gorm:"not null;default:new"`
}

func (r *SearchResult) BeforeCreate(*gorm.DB) error {
	if r.ID == (ulid.ULID{}) {
		r.ID = ulid.Make()
	}
	if r.Status == "" {
		r.Status = SearchResultNew
	}

	return nil
}
