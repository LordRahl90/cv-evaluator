package models

import "gorm.io/gorm"

const (
	StatusPending   = "pending"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

type Job struct {
	ID         int `gorm:"primaryKey"`
	JobID      string
	Provider   string
	Title      string
	Company    string
	Location   string
	PostedAt   string
	DetailsURL string
	Visited    bool
	Status     string

	gorm.Model
}
