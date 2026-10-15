package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Evaluation is the LLM's assessment of one CV against one search result's
// job. There is at most one evaluation per (search result, CV), so
// re-evaluating updates it in place.
//
// Status is StatusPending, StatusCompleted or StatusFailed; Error holds the
// failure reason. RawResponse keeps the LLM output verbatim, including
// output that failed to parse.
type Evaluation struct {
	ID              ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SearchResultID  ulid.ULID `gorm:"type:bytea;not null;uniqueIndex:idx_evaluations_result_cv,priority:1"`
	CVID            ulid.ULID `gorm:"type:bytea;not null;index;uniqueIndex:idx_evaluations_result_cv,priority:2"`
	OverallScore    int
	SkillsScore     int
	ExperienceScore int
	SeniorityScore  int
	DomainScore     int
	LocationScore   int
	Strengths       StringList `gorm:"type:jsonb;not null;default:'[]'"`
	MissingSkills   StringList `gorm:"type:jsonb;not null;default:'[]'"`
	Summary         string     `gorm:"type:text"`
	RawResponse     string     `gorm:"type:text"`
	Model           string
	PromptVersion   string
	Status          string `gorm:"not null;index"`
	Error           string `gorm:"type:text"`
}

func (e *Evaluation) BeforeCreate(*gorm.DB) error {
	if e.ID == (ulid.ULID{}) {
		e.ID = ulid.Make()
	}
	if e.Status == "" {
		e.Status = StatusPending
	}

	return nil
}

// StringList is a list of strings stored as a JSON array.
type StringList []string

// Value implements driver.Valuer. A nil list is stored as an empty array.
func (l StringList) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]string(l))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (l *StringList) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		*l = nil
		return nil
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("scan StringList: unsupported type %T", src)
	}
	return json.Unmarshal(b, (*[]string)(l))
}
