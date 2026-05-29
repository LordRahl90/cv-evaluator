package models

import (
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
)

const (
	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

type Job struct {
	UserID     uint
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

	gorm.Model

	User *User `gorm:"foreignKey:UserID;references:ID"`
}
