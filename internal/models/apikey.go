package models

import (
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// APIKey is a personal API key. Only a SHA-256 hash of the secret is stored;
// the full key is shown to the user once, at creation time. Revoking a key
// soft-deletes the row so it stops authenticating but stays auditable.
type APIKey struct {
	ID        ulid.ULID `gorm:"type:bytea;primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UserID     ulid.ULID `gorm:"type:bytea;not null;index"`
	Name       string    `gorm:"not null"`
	Prefix     string    `gorm:"not null"`
	KeyHash    string    `gorm:"not null;uniqueIndex"`
	LastUsedAt *time.Time
}

func (k *APIKey) BeforeCreate(*gorm.DB) error {
	if k.ID == (ulid.ULID{}) {
		k.ID = ulid.Make()
	}

	return nil
}
