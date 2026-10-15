package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// How often a saved job search runs.
const (
	FrequencyHourly = "hourly"
	FrequencyDaily  = "daily"
	FrequencyWeekly = "weekly"
)

// Frequencies lists every valid JobSearch.Frequency value, shortest first.
var Frequencies = []string{FrequencyHourly, FrequencyDaily, FrequencyWeekly}

// FrequencyInterval returns the time between runs for frequency, and false
// when frequency is not one of the Frequency constants.
func FrequencyInterval(frequency string) (time.Duration, bool) {
	switch frequency {
	case FrequencyHourly:
		return time.Hour, true
	case FrequencyDaily:
		return 24 * time.Hour, true
	case FrequencyWeekly:
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

// JobSearch is a saved search a user runs on a schedule. Every run searches
// all supported job portals; users do not pick one. The scheduler picks up
// enabled searches whose NextRunAt is due.
type JobSearch struct {
	ID        ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
	UserID    ulid.ULID      `gorm:"type:bytea;index"`
	Name      string
	Query     string
	Location  string
	Frequency string // "hourly" | "daily" | "weekly"
	Enabled   bool
	LastRunAt *time.Time
	NextRunAt *time.Time `gorm:"index"`
}

func (j *JobSearch) BeforeCreate(*gorm.DB) error {
	if j.ID == (ulid.ULID{}) {
		j.ID = ulid.Make()
	}

	return nil
}

// NextRun returns when the search should run next after a run at from.
// An unknown frequency falls back to daily.
func (j *JobSearch) NextRun(from time.Time) time.Time {
	interval, ok := FrequencyInterval(j.Frequency)
	if !ok {
		interval = 24 * time.Hour
	}
	return from.Add(interval)
}
