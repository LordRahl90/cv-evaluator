package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Queue task states. A task is pending until a worker claims it, running
// while a worker holds its lease, then completed, or dead once it has
// failed MaxAttempts times. A failed task with attempts left goes back to
// pending with a later RunAt.
const (
	TaskPending   = "pending"
	TaskRunning   = "running"
	TaskCompleted = "completed"
	TaskDead      = "dead"
)

// QueueTask is a unit of background work in the Postgres-backed queue (see
// internal/queue). UniqueKey, when set, is unique among pending and running
// tasks, so enqueueing the same key twice while the first is still queued
// is a no-op.
type QueueTask struct {
	ID          ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Kind        string    `gorm:"not null;index"`
	Payload     string    `gorm:"type:jsonb;not null;default:'{}'"`
	State       string    `gorm:"not null;index:idx_queue_tasks_claim,priority:1"`
	RunAt       time.Time `gorm:"not null;index:idx_queue_tasks_claim,priority:2"`
	UniqueKey   *string   `gorm:"uniqueIndex:idx_queue_tasks_unique_key,where:state = 'pending' OR state = 'running'"`
	Attempts    int       `gorm:"not null;default:0"`
	MaxAttempts int       `gorm:"not null"`
	LockedBy    string
	LockedUntil *time.Time `gorm:"index"`
	LastError   string     `gorm:"type:text"`
	FinishedAt  *time.Time
}

func (QueueTask) TableName() string {
	return "queue_tasks"
}

func (t *QueueTask) BeforeCreate(*gorm.DB) error {
	if t.ID == (ulid.ULID{}) {
		t.ID = ulid.Make()
	}

	return nil
}
