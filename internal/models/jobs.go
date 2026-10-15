package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

const (
	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// Job is a job posting scraped from a provider. Jobs are global: a posting
// is stored, and embedded, once no matter how many users' searches find it.
// It is identified by its provider and the provider's own id (JobID). Users
// reach a job through a SearchResult.
//
// Status is StatusPending until the description is embedded, then
// StatusCompleted. Embedding stays nil until then.
type Job struct {
	ID         ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt `gorm:"index"`
	Provider   string         `gorm:"not null;uniqueIndex:idx_jobs_provider_job_id,priority:1"`
	JobID      string         `gorm:"not null;uniqueIndex:idx_jobs_provider_job_id,priority:2"`
	Title      string
	Company    string
	Location   string
	PostedAt   string
	DetailsURL string
	Visited    bool
	Status     string
	Detail     string           `gorm:"type:text"`
	Embedding  *pgvector.Vector `gorm:"type:vector(768)"`
}

// IsEmbedded reports whether the job's description has been embedded.
func (j *Job) IsEmbedded() bool {
	return j.Embedding != nil && len(j.Embedding.Slice()) > 0
}

func (j *Job) BeforeCreate(*gorm.DB) error {
	if j.ID == (ulid.ULID{}) {
		j.ID = ulid.Make()
	}

	return nil
}
