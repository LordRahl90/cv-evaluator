package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

// Generic statuses, used by Evaluation.
const (
	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// Job statuses. A job is new until its description is fetched, described
// until it is embedded, then embedded. While a worker handles it, it is
// processing (ClaimedAt says since when). It is failed once processing has
// failed MaxAttempts times.
const (
	JobStatusNew        = "new"
	JobStatusDescribed  = "described"
	JobStatusProcessing = "processing"
	JobStatusEmbedded   = "embedded"
	JobStatusFailed     = "failed"
)

// Job is a job posting scraped from a provider. Jobs are global: a posting
// is stored, and embedded, once no matter how many users' searches find it.
// It is identified by its provider and the provider's own id (JobID). Users
// reach a job through a SearchResult.
//
// Status, Attempts, NextAttemptAt, ClaimedAt and LastError track
// background processing: a worker claims a new or described job whose
// NextAttemptAt is due (or one stuck in processing past a timeout). Attempts
// counts claims. Embedding stays nil until the job is embedded.
type Job struct {
	ID            ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`
	Provider      string         `gorm:"not null;uniqueIndex:idx_jobs_provider_job_id,priority:1"`
	JobID         string         `gorm:"not null;uniqueIndex:idx_jobs_provider_job_id,priority:2"`
	Title         string
	Company       string
	Location      string
	PostedAt      string
	DetailsURL    string
	Visited       bool
	Status        string     `gorm:"not null;default:new;index:idx_jobs_work,priority:1"`
	Attempts      int        `gorm:"not null;default:0"`
	NextAttemptAt *time.Time `gorm:"index:idx_jobs_work,priority:2"`
	ClaimedAt     *time.Time
	LastError     string           `gorm:"type:text"`
	Detail        string           `gorm:"type:text"`
	Embedding     *pgvector.Vector `gorm:"type:vector(768)"`
}

// IsEmbedded reports whether the job's description has been embedded.
func (j *Job) IsEmbedded() bool {
	return j.Embedding != nil && len(j.Embedding.Slice()) > 0
}

func (j *Job) BeforeCreate(*gorm.DB) error {
	if j.ID == (ulid.ULID{}) {
		j.ID = ulid.Make()
	}
	if j.Status == "" {
		j.Status = JobStatusNew
		if j.Detail != "" {
			j.Status = JobStatusDescribed
		}
	}

	return nil
}
