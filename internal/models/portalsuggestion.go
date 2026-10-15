package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Review status of a portal suggestion.
const (
	PortalSuggestionSubmitted = "submitted"
	PortalSuggestionPlanned   = "planned"
	PortalSuggestionAdded     = "added"
	PortalSuggestionDeclined  = "declined"
)

// PortalSuggestion is a job portal a user would like searched. The team
// reviews suggestions and builds scrapers for the ones it accepts.
//
// URL is stored normalized, and a user can suggest the same URL only once.
type PortalSuggestion struct {
	ID        ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	UserID    ulid.ULID `gorm:"type:bytea;not null;uniqueIndex:idx_portal_suggestions_user_url"`
	Name      string    `gorm:"not null"`
	URL       string    `gorm:"not null;uniqueIndex:idx_portal_suggestions_user_url"`
	Note      string
	Status    string `gorm:"not null;default:submitted;index"`
}

func (p *PortalSuggestion) BeforeCreate(*gorm.DB) error {
	if p.ID == (ulid.ULID{}) {
		p.ID = ulid.Make()
	}
	if p.Status == "" {
		p.Status = PortalSuggestionSubmitted
	}

	return nil
}
