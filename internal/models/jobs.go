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

type Job struct {
	ID         ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt `gorm:"index"`
	UserID     ulid.ULID      `gorm:"type:bytea;index"`
	JobID      string
	Provider   string
	Title      string
	Company    string
	Location   string
	PostedAt   string
	DetailsURL string
	Visited    bool
	Status     string
	Detail     string          `gorm:"type:text"`
	Embedding  pgvector.Vector `gorm:"type:vector(768)"`

	User *User `gorm:"foreignKey:UserID;references:ID"`
}

func (j *Job) BeforeCreate(*gorm.DB) error {
	if j.ID == (ulid.ULID{}) {
		j.ID = ulid.Make()
	}

	return nil
}
